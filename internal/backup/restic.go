package backup

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type resticConfig struct {
	Repositories       []string
	InitRepositories   bool
	DefaultForgetArgs  string
	RequireSecureRepos bool
	BackupPaths        []string
	Repository         string
	Password           string
	RepoValues         map[string]map[string]string
	RequireRemote      bool
}

type ResticResult struct {
	Configured      bool
	RemoteDelivered bool
	RepositoryCount int
}

const (
	defaultResticCacheHome = "/var/cache/admin-node/restic"
	resticLayoutTag        = "backup-layout:relative-v1"
	resticIntegrityParts   = 4
	resticIntegrityState   = "restic-integrity-subset.json"
)

type resticBackupSpec struct {
	Paths      []string
	WorkingDir string
	BackupID   string
	Kind       string
	Relative   bool
}

type integritySubsetState struct {
	Version    int `json:"version"`
	NextSubset int `json:"next_subset"`
}

func RunRestic(ctx context.Context, envFile string, backupPaths []string) (ResticResult, error) {
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		return ResticResult{}, err
	}
	if _, err := exec.LookPath("restic"); err != nil {
		if cfg.RequireRemote {
			return ResticResult{}, fmt.Errorf("restic is required by BACKUP_REQUIRE_REMOTE_REPOSITORY but is not installed")
		}
		fmt.Println("[restic] restic is not installed, skipping optional repository backups")
		return ResticResult{}, nil
	}
	if err := ensureResticCacheEnv(); err != nil {
		return ResticResult{}, err
	}
	if len(backupPaths) > 0 {
		cfg.BackupPaths = backupPaths
	}
	if len(cfg.BackupPaths) == 0 {
		cfg.BackupPaths = []string{"/srv/admin/stacks", "/srv/admin/env", "/srv/admin/data"}
	}
	if cfg.DefaultForgetArgs == "" {
		cfg.DefaultForgetArgs = "--keep-last 3"
	}
	spec, err := newResticBackupSpec(cfg.BackupPaths)
	if err != nil {
		return ResticResult{}, err
	}

	if len(cfg.Repositories) > 0 {
		for _, repoID := range cfg.Repositories {
			if err := runResticRepo(ctx, cfg, repoID, spec); err != nil {
				return ResticResult{}, err
			}
		}
		return ResticResult{Configured: true, RemoteDelivered: hasRemoteRepository(cfg), RepositoryCount: len(cfg.Repositories)}, nil
	}
	if cfg.Repository != "" {
		if err := runResticLegacy(ctx, cfg, spec); err != nil {
			return ResticResult{}, err
		}
		return ResticResult{Configured: true, RemoteDelivered: hasRemoteRepository(cfg), RepositoryCount: 1}, nil
	}
	fmt.Println("[restic] no repositories configured, skipping remote backup")
	return ResticResult{}, nil
}

func CheckRestic(ctx context.Context, envFile, statusRoot string) (ResticResult, error) {
	if _, err := exec.LookPath("restic"); err != nil {
		return ResticResult{}, fmt.Errorf("restic is required for integrity checks")
	}
	if err := ensureResticCacheEnv(); err != nil {
		return ResticResult{}, err
	}
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		return ResticResult{}, err
	}
	if len(cfg.Repositories) == 0 && cfg.Repository == "" {
		fmt.Println("[restic] no repositories configured, skipping integrity check")
		return ResticResult{}, nil
	}
	subset, err := loadIntegritySubset(statusRoot)
	if err != nil {
		return ResticResult{}, err
	}
	readSubset := fmt.Sprintf("%d/%d", subset, resticIntegrityParts)
	for _, repoID := range cfg.Repositories {
		values := cfg.RepoValues[sanitizeRepoID(repoID)]
		repo, password := values["RESTIC_REPOSITORY"], values["RESTIC_PASSWORD"]
		if repo == "" || password == "" {
			return ResticResult{}, fmt.Errorf("incomplete restic repository %q", repoID)
		}
		if err := validateSecureRepository(repo, cfg.RequireSecureRepos); err != nil {
			return ResticResult{}, err
		}
		fmt.Printf("[restic] checking repository '%s' data subset %s\n", repoID, readSubset)
		args := append(fields(values["RESTIC_OPTIONS"]), "check", "--read-data-subset", readSubset)
		if err := restic(ctx, repoEnv(values, repo, password), args...); err != nil {
			return ResticResult{}, fmt.Errorf("check restic repository %q: %w", repoID, err)
		}
	}
	if cfg.Repository != "" {
		if cfg.Password == "" {
			return ResticResult{}, fmt.Errorf("RESTIC_PASSWORD is required when RESTIC_REPOSITORY is set")
		}
		fmt.Printf("[restic] checking legacy repository data subset %s\n", readSubset)
		env := append(os.Environ(), "RESTIC_REPOSITORY="+cfg.Repository, "RESTIC_PASSWORD="+cfg.Password)
		if err := restic(ctx, env, "check", "--read-data-subset", readSubset); err != nil {
			return ResticResult{}, fmt.Errorf("check legacy restic repository: %w", err)
		}
	}
	for _, repoID := range cfg.Repositories {
		values := cfg.RepoValues[sanitizeRepoID(repoID)]
		repo, password := values["RESTIC_REPOSITORY"], values["RESTIC_PASSWORD"]
		fmt.Printf("[restic] pruning repository '%s' after successful integrity checks\n", repoID)
		if err := restic(ctx, repoEnv(values, repo, password), append(fields(values["RESTIC_OPTIONS"]), "prune")...); err != nil {
			return ResticResult{}, fmt.Errorf("prune restic repository %q: %w", repoID, err)
		}
	}
	if cfg.Repository != "" {
		fmt.Println("[restic] pruning legacy repository after successful integrity checks")
		env := append(os.Environ(), "RESTIC_REPOSITORY="+cfg.Repository, "RESTIC_PASSWORD="+cfg.Password)
		if err := restic(ctx, env, "prune"); err != nil {
			return ResticResult{}, fmt.Errorf("prune legacy restic repository: %w", err)
		}
	}
	count := len(cfg.Repositories)
	if cfg.Repository != "" {
		count++
	}
	if err := storeNextIntegritySubset(statusRoot, subset); err != nil {
		return ResticResult{}, err
	}
	return ResticResult{Configured: true, RemoteDelivered: hasRemoteRepository(cfg), RepositoryCount: count}, nil
}

func ensureResticCacheEnv() error {
	if os.Getenv("XDG_CACHE_HOME") != "" || os.Getenv("HOME") != "" {
		return nil
	}
	cacheHome := os.Getenv("ADMIN_NODE_RESTIC_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = defaultResticCacheHome
	}
	if err := os.MkdirAll(cacheHome, 0o700); err != nil {
		return fmt.Errorf("create restic cache directory: %w", err)
	}
	return os.Setenv("XDG_CACHE_HOME", cacheHome)
}

func loadResticConfig(path string) (resticConfig, error) {
	cfg := resticConfig{RequireSecureRepos: true, RepoValues: map[string]map[string]string{}}
	values, err := parseEnvFile(path)
	if err != nil {
		return cfg, err
	}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok && strings.HasPrefix(key, "RESTIC_") {
			values[key] = value
		}
	}
	cfg.Repositories = fields(values["RESTIC_REPOSITORIES"])
	cfg.InitRepositories = parseBool(values["RESTIC_INIT_REPOSITORIES"], false)
	cfg.DefaultForgetArgs = values["RESTIC_DEFAULT_FORGET_ARGS"]
	cfg.RequireSecureRepos = parseBool(values["RESTIC_REQUIRE_SECURE_REPOSITORIES"], true)
	cfg.RequireRemote = parseBool(values["BACKUP_REQUIRE_REMOTE_REPOSITORY"], false)
	cfg.BackupPaths = fields(values["RESTIC_BACKUP_PATHS"])
	cfg.Repository = values["RESTIC_REPOSITORY"]
	cfg.Password = values["RESTIC_PASSWORD"]
	for key, value := range values {
		prefix, id, ok := splitRepoVar(key)
		if !ok {
			continue
		}
		if cfg.RepoValues[id] == nil {
			cfg.RepoValues[id] = map[string]string{}
		}
		cfg.RepoValues[id][prefix] = value
	}
	if cfg.RequireRemote && !hasRemoteRepository(cfg) {
		return cfg, fmt.Errorf("at least one non-local restic repository is required")
	}
	return cfg, nil
}

func hasRemoteRepository(cfg resticConfig) bool {
	for _, id := range cfg.Repositories {
		repo := cfg.RepoValues[sanitizeRepoID(id)]["RESTIC_REPOSITORY"]
		if isRemoteRepository(repo) {
			return true
		}
	}
	return isRemoteRepository(cfg.Repository)
}

func isRemoteRepository(repository string) bool {
	if repository == "" || filepath.IsAbs(repository) || strings.HasPrefix(repository, ".") || strings.HasPrefix(repository, "file:") {
		return false
	}
	return strings.Contains(repository, ":")
}

func parseEnvFile(path string) (map[string]string, error) {
	values := map[string]string{}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = unquoteEnvValue(value)
		values[key] = value
	}
	return values, scanner.Err()
}

func unquoteEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
	}
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}

func splitRepoVar(key string) (string, string, bool) {
	prefixes := []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_DEFAULT_REGION",
		"RESTIC_REST_USERNAME", "RESTIC_REST_PASSWORD",
		"B2_ACCOUNT_ID", "B2_ACCOUNT_KEY",
		"GOOGLE_PROJECT_ID", "GOOGLE_APPLICATION_CREDENTIALS",
		"AZURE_ACCOUNT_NAME", "AZURE_ACCOUNT_KEY", "AZURE_ACCOUNT_SAS", "AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET", "AZURE_TENANT_ID",
		"OS_AUTH_URL", "OS_REGION_NAME", "OS_USERNAME", "OS_PASSWORD", "OS_TENANT_ID", "OS_TENANT_NAME", "OS_USER_ID", "OS_USER_DOMAIN_NAME", "OS_USER_DOMAIN_ID", "OS_PROJECT_NAME", "OS_PROJECT_DOMAIN_NAME",
		"ST_AUTH", "ST_USER", "ST_KEY",
		"RESTIC_REPOSITORY", "RESTIC_PASSWORD", "RESTIC_FORGET_ARGS", "RESTIC_OPTIONS",
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	for _, prefix := range prefixes {
		suffix := strings.TrimPrefix(key, prefix+"_")
		if suffix != key && suffix != "" {
			return prefix, suffix, true
		}
	}
	return "", "", false
}

func runResticRepo(ctx context.Context, cfg resticConfig, id string, spec resticBackupSpec) error {
	safeID := sanitizeRepoID(id)
	values := cfg.RepoValues[safeID]
	repo := values["RESTIC_REPOSITORY"]
	password := values["RESTIC_PASSWORD"]
	if repo == "" {
		return fmt.Errorf("RESTIC_REPOSITORY_%s is required", safeID)
	}
	if password == "" {
		return fmt.Errorf("RESTIC_PASSWORD_%s is required", safeID)
	}
	if err := validateSecureRepository(repo, cfg.RequireSecureRepos); err != nil {
		return err
	}
	env := repoEnv(values, repo, password)
	options := fields(values["RESTIC_OPTIONS"])
	if err := initRestic(ctx, cfg, env, options, id); err != nil {
		return err
	}
	fmt.Printf("[restic] backing up to repository '%s'\n", id)
	verificationTag := fmt.Sprintf("admin-node-run:%d", time.Now().UTC().UnixNano())
	formatTag := "admin-node-v2"
	if spec.Relative {
		formatTag = "admin-node-v3"
	}
	backupArgs := append(append([]string{}, options...), "backup", "--tag", formatTag, "--tag", verificationTag)
	if spec.BackupID != "" {
		backupArgs = append(backupArgs, "--tag", "backup-id:"+spec.BackupID)
	}
	if spec.Relative {
		backupArgs = append(backupArgs, "--ignore-inode", "--no-scan", "--tag", resticLayoutTag, "--tag", resticKindTag(spec.Kind))
		parent, err := latestCompatibleParent(ctx, env, options, spec.Kind)
		if err != nil {
			return fmt.Errorf("find compatible parent in repository %q: %w", id, err)
		}
		if parent != "" {
			backupArgs = append(backupArgs, "--parent", parent)
		}
	}
	if err := resticInDir(ctx, env, spec.WorkingDir, append(backupArgs, spec.Paths...)...); err != nil {
		return err
	}
	if err := verifyResticSnapshot(ctx, env, options, verificationTag); err != nil {
		return fmt.Errorf("verify repository '%s' delivery: %w", id, err)
	}
	forgetArgs := values["RESTIC_FORGET_ARGS"]
	if forgetArgs == "" {
		forgetArgs = cfg.DefaultForgetArgs
	}
	if spec.Relative {
		return forgetResticLayout(ctx, env, options, forgetArgs, spec.Kind)
	}
	return forgetRestic(ctx, env, options, forgetArgs)
}

func newResticBackupSpec(paths []string) (resticBackupSpec, error) {
	spec := resticBackupSpec{Paths: append([]string{}, paths...)}
	if len(paths) != 1 {
		return spec, nil
	}
	root := filepath.Clean(paths[0])
	spec.BackupID = backupIDFromPaths(paths)
	manifest, ok, err := ReadManifest(root)
	if err != nil {
		return spec, err
	}
	if !ok || manifest.Version != ManifestVersion {
		return spec, nil
	}
	manifest, err = Verify(root)
	if err != nil {
		return spec, fmt.Errorf("verify relative-layout backup: %w", err)
	}
	if manifest.ID != spec.BackupID {
		return spec, fmt.Errorf("manifest id %q does not match backup path", manifest.ID)
	}
	spec.Relative = true
	spec.WorkingDir = root
	spec.Paths = []string{"."}
	spec.Kind = "standard"
	if manifest.OfflineImages {
		spec.Kind = "offline-images"
	}
	return spec, nil
}

func resticKindTag(kind string) string {
	return "backup-kind:" + kind
}

func latestCompatibleParent(ctx context.Context, env, options []string, kind string) (string, error) {
	tags := resticLayoutTag + "," + resticKindTag(kind)
	args := append(append([]string{}, options...), "snapshots", "--json", "--tag", tags)
	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var snapshots []struct {
		ID   string    `json:"id"`
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return "", fmt.Errorf("decode compatible snapshot inventory: %w", err)
	}
	latestID := ""
	latestTime := time.Time{}
	for _, snapshot := range snapshots {
		if snapshot.ID == "" || snapshot.Time.IsZero() {
			return "", fmt.Errorf("compatible snapshot inventory contains an invalid entry")
		}
		if snapshot.Time.After(latestTime) || (snapshot.Time.Equal(latestTime) && snapshot.ID > latestID) {
			latestID = snapshot.ID
			latestTime = snapshot.Time
		}
	}
	return latestID, nil
}

func backupIDFromPaths(paths []string) string {
	if len(paths) != 1 {
		return ""
	}
	id := filepath.Base(filepath.Clean(paths[0]))
	if ValidID(id) {
		return id
	}
	return ""
}

func RestoreFromRestic(ctx context.Context, envFile, repositoryID, backupRoot, id string) error {
	if !ValidID(id) {
		return fmt.Errorf("a valid explicit backup id is required for remote restore")
	}
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		return err
	}
	values := cfg.RepoValues[sanitizeRepoID(repositoryID)]
	if values == nil {
		return fmt.Errorf("unknown restic repository %q", repositoryID)
	}
	repo, password := values["RESTIC_REPOSITORY"], values["RESTIC_PASSWORD"]
	if repo == "" || password == "" {
		return fmt.Errorf("incomplete restic repository %q", repositoryID)
	}
	if err := validateSecureRepository(repo, cfg.RequireSecureRepos); err != nil {
		return err
	}
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(backupRoot, ".restic-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	env := repoEnv(values, repo, password)
	options := fields(values["RESTIC_OPTIONS"])
	args := append(append([]string{}, options...), "restore", "latest", "--tag", "backup-id:"+id, "--target", tmp, "--verify")
	if err := restic(ctx, env, args...); err != nil {
		return err
	}
	restored := tmp
	if _, ok, err := ReadManifest(restored); err != nil {
		return err
	} else if !ok {
		restored = filepath.Join(tmp, strings.TrimPrefix(filepath.Clean(filepath.Join(backupRoot, id)), string(os.PathSeparator)))
	}
	if _, err := Verify(restored); err != nil {
		return fmt.Errorf("verify remote backup: %w", err)
	}
	target := filepath.Join(backupRoot, id)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("backup already exists: %s", id)
	}
	return os.Rename(restored, target)
}

func runResticLegacy(ctx context.Context, cfg resticConfig, spec resticBackupSpec) error {
	if cfg.Password == "" {
		return fmt.Errorf("RESTIC_PASSWORD is required when RESTIC_REPOSITORY is set")
	}
	if err := validateSecureRepository(cfg.Repository, cfg.RequireSecureRepos); err != nil {
		return err
	}
	env := append(os.Environ(), "RESTIC_REPOSITORY="+cfg.Repository, "RESTIC_PASSWORD="+cfg.Password)
	options := []string{}
	if err := initRestic(ctx, cfg, env, options, "default"); err != nil {
		return err
	}
	fmt.Println("[restic] backing up to legacy RESTIC_REPOSITORY")
	verificationTag := fmt.Sprintf("admin-node-run:%d", time.Now().UTC().UnixNano())
	backupArgs := []string{"backup", "--tag", "admin-node-v2", "--tag", verificationTag}
	if spec.BackupID != "" {
		backupArgs = append(backupArgs, "--tag", "backup-id:"+spec.BackupID)
	}
	if spec.Relative {
		backupArgs[2] = "admin-node-v3"
		backupArgs = append(backupArgs, "--ignore-inode", "--no-scan", "--tag", resticLayoutTag, "--tag", resticKindTag(spec.Kind))
		parent, err := latestCompatibleParent(ctx, env, options, spec.Kind)
		if err != nil {
			return fmt.Errorf("find compatible parent in legacy repository: %w", err)
		}
		if parent != "" {
			backupArgs = append(backupArgs, "--parent", parent)
		}
	}
	if err := resticInDir(ctx, env, spec.WorkingDir, append(backupArgs, spec.Paths...)...); err != nil {
		return err
	}
	if err := verifyResticSnapshot(ctx, env, nil, verificationTag); err != nil {
		return fmt.Errorf("verify legacy repository delivery: %w", err)
	}
	if spec.Relative {
		return forgetResticLayout(ctx, env, options, cfg.DefaultForgetArgs, spec.Kind)
	}
	return forgetRestic(ctx, env, options, cfg.DefaultForgetArgs)
}

func verifyResticSnapshot(ctx context.Context, env, options []string, tag string) error {
	args := append(append([]string{}, options...), "snapshots", "--json", "--tag", tag)
	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("query tagged snapshot: %w", err)
	}
	var snapshots []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return fmt.Errorf("decode tagged snapshot inventory: %w", err)
	}
	if len(snapshots) != 1 || snapshots[0].ID == "" {
		return fmt.Errorf("expected exactly one tagged snapshot, found %d", len(snapshots))
	}
	return nil
}

func initRestic(ctx context.Context, cfg resticConfig, env []string, options []string, id string) error {
	if !cfg.InitRepositories {
		return nil
	}
	if err := resticQuiet(ctx, env, append(options, "cat", "config")...); err == nil {
		return nil
	}
	fmt.Printf("[restic] initializing repository '%s'\n", id)
	return restic(ctx, env, append(options, "init")...)
}

func forgetRestic(ctx context.Context, env []string, options []string, forgetArgs string) error {
	if forgetArgs == "none" {
		return nil
	}
	retentionArgs := forgetRetentionArgs(forgetArgs)
	if len(retentionArgs) == 0 {
		return nil
	}
	return restic(ctx, env, append(append(options, "forget"), retentionArgs...)...)
}

func forgetResticLayout(ctx context.Context, env []string, options []string, forgetArgs, kind string) error {
	if forgetArgs == "none" || strings.TrimSpace(forgetArgs) == "" {
		return nil
	}
	retentionArgs := forgetRetentionArgs(forgetArgs)
	if len(retentionArgs) == 0 {
		return nil
	}
	args := append(append([]string{}, options...), "forget")
	args = append(args, retentionArgs...)
	args = append(args, "--tag", resticLayoutTag+","+resticKindTag(kind), "--group-by", "")
	return restic(ctx, env, args...)
}

func forgetRetentionArgs(value string) []string {
	args := fields(value)
	retentionArgs := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--prune" || strings.HasPrefix(arg, "--prune=") {
			continue
		}
		retentionArgs = append(retentionArgs, arg)
	}
	return retentionArgs
}

func repoEnv(values map[string]string, repo, password string) []string {
	env := append(os.Environ(), "RESTIC_REPOSITORY="+repo, "RESTIC_PASSWORD="+password)
	for key, value := range values {
		if strings.HasPrefix(key, "RESTIC_") {
			continue
		}
		env = append(env, key+"="+value)
	}
	return env
}

func restic(ctx context.Context, env []string, args ...string) error {
	return resticInDir(ctx, env, "", args...)
}

func resticInDir(ctx context.Context, env []string, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func loadIntegritySubset(statusRoot string) (int, error) {
	path := filepath.Join(statusRoot, resticIntegrityState)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read restic integrity state: %w", err)
	}
	var state integritySubsetState
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, fmt.Errorf("decode restic integrity state: %w", err)
	}
	if state.Version != 1 || state.NextSubset < 1 || state.NextSubset > resticIntegrityParts {
		return 0, fmt.Errorf("invalid restic integrity state")
	}
	return state.NextSubset, nil
}

func storeNextIntegritySubset(statusRoot string, completed int) error {
	if err := os.MkdirAll(statusRoot, 0o700); err != nil {
		return fmt.Errorf("create backup status directory: %w", err)
	}
	next := completed%resticIntegrityParts + 1
	data, err := json.MarshalIndent(integritySubsetState{Version: 1, NextSubset: next}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(statusRoot, ".restic-integrity-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(statusRoot, resticIntegrityState)); err != nil {
		return fmt.Errorf("publish restic integrity state: %w", err)
	}
	return nil
}

func resticQuiet(ctx context.Context, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = env
	return cmd.Run()
}

func validateSecureRepository(repo string, requireSecure bool) error {
	if !requireSecure || !strings.Contains(repo, ":") {
		return nil
	}
	switch {
	case strings.HasPrefix(repo, "/"), strings.HasPrefix(repo, "."):
		return nil
	case strings.HasPrefix(repo, "sftp:"), strings.HasPrefix(repo, "rest:https://"), strings.HasPrefix(repo, "s3:s3."), strings.HasPrefix(repo, "s3:https://"), strings.HasPrefix(repo, "swift:"), strings.HasPrefix(repo, "b2:"), strings.HasPrefix(repo, "azure:"), strings.HasPrefix(repo, "gs:"):
		return nil
	case strings.HasPrefix(repo, "rest:http://"), strings.HasPrefix(repo, "s3:http://"), strings.HasPrefix(repo, "ftp:"):
		return fmt.Errorf("refusing insecure restic repository URL")
	case strings.HasPrefix(repo, "rclone:"):
		return fmt.Errorf("refusing rclone repository while RESTIC_REQUIRE_SECURE_REPOSITORIES=true")
	default:
		return fmt.Errorf("unsupported or insecure restic repository URL")
	}
}

func sanitizeRepoID(id string) string {
	id = strings.ToUpper(id)
	var b strings.Builder
	for _, r := range id {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func fields(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Fields(value)
}

func parseBool(value string, fallback bool) bool {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func defaultBackupPaths(adminRoot string) []string {
	return []string{filepath.Join(adminRoot, "stacks"), filepath.Join(adminRoot, "env"), filepath.Join(adminRoot, "data")}
}
