package backup

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
}

func RunRestic(ctx context.Context, envFile string, backupPaths []string) error {
	if _, err := exec.LookPath("restic"); err != nil {
		fmt.Println("[restic] restic is not installed, skipping remote backups")
		return nil
	}
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		return err
	}
	if len(backupPaths) > 0 {
		cfg.BackupPaths = backupPaths
	}
	if len(cfg.BackupPaths) == 0 {
		cfg.BackupPaths = []string{"/srv/admin/stacks", "/srv/admin/env", "/srv/admin/data"}
	}
	if cfg.DefaultForgetArgs == "" {
		cfg.DefaultForgetArgs = "--keep-last 3 --prune"
	}

	if len(cfg.Repositories) > 0 {
		for _, repoID := range cfg.Repositories {
			if err := runResticRepo(ctx, cfg, repoID); err != nil {
				return err
			}
		}
		return nil
	}
	if cfg.Repository != "" {
		return runResticLegacy(ctx, cfg)
	}
	fmt.Println("[restic] no repositories configured, skipping remote backup")
	return nil
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
	return cfg, nil
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
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		values[key] = value
	}
	return values, scanner.Err()
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

func runResticRepo(ctx context.Context, cfg resticConfig, id string) error {
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
	if err := restic(ctx, env, append(append(options, "backup"), cfg.BackupPaths...)...); err != nil {
		return err
	}
	forgetArgs := values["RESTIC_FORGET_ARGS"]
	if forgetArgs == "" {
		forgetArgs = cfg.DefaultForgetArgs
	}
	return forgetRestic(ctx, env, options, forgetArgs)
}

func runResticLegacy(ctx context.Context, cfg resticConfig) error {
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
	if err := restic(ctx, env, append([]string{"backup"}, cfg.BackupPaths...)...); err != nil {
		return err
	}
	return forgetRestic(ctx, env, options, cfg.DefaultForgetArgs)
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
	if strings.TrimSpace(forgetArgs) == "" {
		return nil
	}
	return restic(ctx, env, append(append(options, "forget"), fields(forgetArgs)...)...)
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
	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
		return fmt.Errorf("refusing insecure repository URL: %s", repo)
	case strings.HasPrefix(repo, "rclone:"):
		return fmt.Errorf("refusing rclone repository while RESTIC_REQUIRE_SECURE_REPOSITORIES=true: %s", repo)
	default:
		return fmt.Errorf("unsupported or insecure repository URL: %s", repo)
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
