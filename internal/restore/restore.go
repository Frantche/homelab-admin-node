package restore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/backup"
	"github.com/Frantche/homelab-admin-node/internal/config"
	adminmode "github.com/Frantche/homelab-admin-node/internal/mode"
	"github.com/Frantche/homelab-admin-node/internal/openbao"
	"github.com/Frantche/homelab-admin-node/internal/operation"
)

type Options struct {
	ID                    string
	Validate              func(context.Context) error
	RestoreHarborWritable func(context.Context) error
	Out                   io.Writer
	LockFile              string
	SystemdTimers         []string
}

func Run(ctx context.Context, cfg config.Config, opts Options) error {
	currentMode, err := adminmode.Read(cfg.ModeFile)
	if err != nil || currentMode != "restore" {
		return fmt.Errorf("refusing restore unless mode is restore")
	}
	unlock, err := operation.Acquire(opts.LockFile)
	if err != nil {
		writeMode(cfg.ModeFile, "restore_failed")
		return err
	}
	defer unlock()
	resumeTimers, err := suspendSystemdTimers(ctx, opts.SystemdTimers)
	if err != nil {
		writeMode(cfg.ModeFile, "restore_failed")
		return err
	}
	restoreSucceeded := false
	defer func() {
		if restoreSucceeded {
			resumeTimers()
		}
	}()

	id := opts.ID
	if id == "" {
		id = restoreIDFromFile(cfg.RestoreIDFile)
	}
	if id == "" {
		id = "latest"
	}

	info, ok, err := Resolve(cfg.BackupRoot, id)
	if err != nil {
		writeMode(cfg.ModeFile, "restore_failed")
		return err
	}
	if !ok {
		writeMode(cfg.ModeFile, "restore_failed")
		return fmt.Errorf("restore source not found")
	}
	if _, err := backup.Verify(info.Path); err != nil {
		writeMode(cfg.ModeFile, "restore_failed")
		return fmt.Errorf("backup verification failed: %w", err)
	}

	set := stackSet(cfg.AdminRoot)
	if err := stopStacks(ctx, cfg, set); err != nil {
		writeMode(cfg.ModeFile, "restore_failed")
		return err
	}
	if err := fixOpenBaoDataPermissions(cfg.AdminRoot); err != nil {
		writeMode(cfg.ModeFile, "restore_failed")
		return err
	}

	if fileExists(filepath.Join(info.Path, "offline-images.tar")) {
		if err := run(ctx, nil, "docker", "load", "-i", filepath.Join(info.Path, "offline-images.tar")); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return err
		}
	}

	if dirExists(filepath.Join(info.Path, "gitea-stack")) {
		giteaSource, err := snapshotArtifactSource(filepath.Join(info.Path, "gitea-stack"), ".gitea-", "gitea", "postgres")
		if err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return fmt.Errorf("inspect gitea stack artifact: %w", err)
		}
		if err := replaceDirContents(giteaSource, cfg.GiteaStackPath); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return fmt.Errorf("restore gitea stack: %w", err)
		}
	} else if dirExists(filepath.Join(info.Path, "gitea-data")) {
		giteaDataPath := filepath.Join(cfg.AdminRoot, "data/gitea")
		if err := replaceDirContents(filepath.Join(info.Path, "gitea-data"), giteaDataPath); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return err
		}
		if err := fixGiteaDataPermissions(giteaDataPath); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return err
		}
	}
	if dirExists(filepath.Join(info.Path, "harbor-data")) {
		harborRoot := filepath.Join(cfg.AdminRoot, "data/harbor")
		for _, rel := range []string{"registry", "core", "job_logs", "trivy-adapter/reports"} {
			source := filepath.Join(info.Path, "harbor-data", rel)
			if dirExists(source) {
				// Backups produced before V2 layout normalization wrapped each
				// selected path once under its basename.
				if nested := filepath.Join(source, filepath.Base(source)); dirExists(nested) {
					source = nested
				}
				if err := replaceDirContents(source, filepath.Join(harborRoot, rel)); err != nil {
					writeMode(cfg.ModeFile, "restore_failed")
					return fmt.Errorf("restore harbor data %s: %w", rel, err)
				}
			}
		}
	}

	if fileExists(filepath.Join(info.Path, "keycloak.dump")) {
		if err := restorePostgres(ctx, stackCommand{Compose: set.KeycloakCompose, EnvFile: set.KeycloakEnv}, "keycloak-db", "keycloak", "keycloak", filepath.Join(info.Path, "keycloak.dump")); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return fmt.Errorf("restore keycloak: %w", err)
		}
	}
	if !dirExists(filepath.Join(info.Path, "gitea-stack")) && fileExists(filepath.Join(info.Path, "gitea.dump")) && fileExists(set.GiteaCompose) && fileExists(set.GiteaEnv) {
		if err := restorePostgres(ctx, stackCommand{Compose: set.GiteaCompose, EnvFile: set.GiteaEnv}, "gitea-db", "gitea", "gitea", filepath.Join(info.Path, "gitea.dump")); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return fmt.Errorf("restore gitea: %w", err)
		}
	}
	if fileExists(filepath.Join(info.Path, "harbor.dump")) && fileExists(set.HarborCompose) && fileExists(set.HarborEnv) {
		if err := restorePostgres(ctx, stackCommand{Compose: set.HarborCompose, EnvFile: set.HarborEnv}, "harbor-db", "postgres", "registry", filepath.Join(info.Path, "harbor.dump")); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return fmt.Errorf("restore harbor: %w", err)
		}
	}
	if fileExists(filepath.Join(info.Path, "openbao.snap")) {
		if err := restoreOpenBao(ctx, cfg, set.OpenBaoCompose, filepath.Join(info.Path, "openbao.snap")); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return fmt.Errorf("restore openbao: %w", err)
		}
	}

	if err := startStacks(ctx, cfg, set); err != nil {
		writeMode(cfg.ModeFile, "restore_failed")
		return err
	}
	if fileExists(set.HarborCompose) && opts.RestoreHarborWritable != nil {
		if err := opts.RestoreHarborWritable(ctx); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return fmt.Errorf("restore Harbor writable mode: %w", err)
		}
	}
	if fileExists(set.OpenBaoCompose) {
		if err := unsealOpenBao(ctx, cfg); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return err
		}
	}
	if opts.Validate != nil {
		if err := opts.Validate(ctx); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return err
		}
	}
	if err := writeMode(cfg.ModeFile, "normal"); err != nil {
		return err
	}
	if opts.Out != nil {
		fmt.Fprintln(opts.Out, "Restore completed and mode set to normal")
	}
	restoreSucceeded = true
	return nil
}

func RestoreHarborWritable(ctx context.Context, cfg config.Config) error {
	user := os.Getenv("HARBOR_ADMIN_USER")
	password := os.Getenv("HARBOR_ADMIN_PASSWORD")
	if user == "" {
		user = envFileValue(filepath.Join(cfg.AdminRoot, "env/harbor.env"), "HARBOR_ADMIN_USER")
	}
	if user == "" {
		user = "admin"
	}
	if password == "" {
		password = envFileValue(filepath.Join(cfg.AdminRoot, "env/harbor.env"), "HARBOR_ADMIN_PASSWORD")
	}
	if password == "" {
		return fmt.Errorf("Harbor admin password is unavailable")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var lastErr error
	for {
		lastErr = backup.SetHarborReadOnly(waitCtx, cfg.HarborDomain, user, password, false)
		if lastErr == nil {
			return nil
		}
		if waitCtx.Err() != nil {
			return lastErr
		}
		time.Sleep(3 * time.Second)
	}
}

func envFileValue(path, key string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			if decoded, err := strconv.Unquote(value); err == nil {
				return decoded
			}
		}
		if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
		return value
	}
	return ""
}

func Resolve(root, id string) (backup.Info, bool, error) {
	if id == "latest" {
		return backup.Latest(root)
	}
	if !backup.ValidID(id) {
		return backup.Info{}, false, fmt.Errorf("invalid backup id: %q", id)
	}
	path := filepath.Join(root, id)
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return backup.Info{}, false, err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return backup.Info{}, false, err
	}
	if filepath.Dir(cleanPath) != cleanRoot {
		return backup.Info{}, false, fmt.Errorf("backup path escapes root")
	}
	if !dirExists(path) {
		return backup.Info{}, false, nil
	}
	info, err := inspectBackup(path, id)
	if err != nil {
		return backup.Info{}, false, err
	}
	return info, true, nil
}

func Select(in io.Reader, out io.Writer, backups []backup.Info) (string, error) {
	if len(backups) == 0 {
		return "", fmt.Errorf("no backups available")
	}
	for i, item := range backups {
		fmt.Fprintf(out, "%d) %s %s\n", i+1, item.ID, item.CreatedAt.Format(time.RFC3339))
	}
	fmt.Fprint(out, "Select backup: ")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "", fmt.Errorf("selection cancelled")
	}
	choice := strings.TrimSpace(scanner.Text())
	for i, item := range backups {
		if choice == fmt.Sprintf("%d", i+1) || choice == item.ID {
			return item.ID, nil
		}
	}
	return "", fmt.Errorf("invalid backup selection")
}

type stacks struct {
	TraefikCompose     string
	TraefikEnv         string
	KeycloakCompose    string
	KeycloakEnv        string
	OpenBaoCompose     string
	HarborCompose      string
	HarborEnv          string
	GiteaCompose       string
	GiteaEnv           string
	CloudflaredCompose string
	CloudflaredEnv     string
}

type stackCommand struct {
	Compose string
	EnvFile string
}

func stackSet(adminRoot string) stacks {
	return stacks{
		TraefikCompose:     filepath.Join(adminRoot, "stacks/traefik/compose.yaml"),
		TraefikEnv:         filepath.Join(adminRoot, "env/traefik.env"),
		KeycloakCompose:    filepath.Join(adminRoot, "stacks/keycloak/compose.yaml"),
		KeycloakEnv:        filepath.Join(adminRoot, "env/keycloak.env"),
		OpenBaoCompose:     filepath.Join(adminRoot, "stacks/openbao/compose.yaml"),
		HarborCompose:      filepath.Join(adminRoot, "stacks/harbor/compose.yaml"),
		HarborEnv:          filepath.Join(adminRoot, "env/harbor.env"),
		GiteaCompose:       filepath.Join(adminRoot, "stacks/gitea/compose.yaml"),
		GiteaEnv:           filepath.Join(adminRoot, "env/gitea.env"),
		CloudflaredCompose: filepath.Join(adminRoot, "stacks/cloudflared/compose.yaml"),
		CloudflaredEnv:     filepath.Join(adminRoot, "env/cloudflared.env"),
	}
}

func stopStacks(ctx context.Context, cfg config.Config, set stacks) error {
	commands := []stackCommand{
		{set.TraefikCompose, set.TraefikEnv},
		{set.KeycloakCompose, set.KeycloakEnv},
		{set.OpenBaoCompose, ""},
		{set.HarborCompose, set.HarborEnv},
		{set.GiteaCompose, set.GiteaEnv},
	}
	if !cfg.CloudflareDisabled {
		commands = append(commands, stackCommand{set.CloudflaredCompose, set.CloudflaredEnv})
	}
	for _, command := range commands {
		if fileExists(command.Compose) {
			if err := dockerCompose(ctx, command, "down"); err != nil {
				return fmt.Errorf("stop stack %s: %w", command.Compose, err)
			}
		}
	}
	return nil
}

func startStacks(ctx context.Context, cfg config.Config, set stacks) error {
	commands := []stackCommand{
		{set.OpenBaoCompose, ""},
		{set.TraefikCompose, set.TraefikEnv},
		{set.KeycloakCompose, set.KeycloakEnv},
		{set.HarborCompose, set.HarborEnv},
		{set.GiteaCompose, set.GiteaEnv},
	}
	if !cfg.CloudflareDisabled && !cfg.CIMockCloudflareTunnel {
		commands = append(commands, stackCommand{set.CloudflaredCompose, set.CloudflaredEnv})
	}
	for _, command := range commands {
		if fileExists(command.Compose) {
			if command.Compose == set.CloudflaredCompose {
				_ = removeStoppedContainer(ctx, "cloudflared")
			}
			if err := dockerCompose(ctx, command, "up", "-d"); err != nil {
				return err
			}
		}
	}
	return nil
}

func suspendSystemdTimers(ctx context.Context, timers []string) (func(), error) {
	if len(timers) == 0 {
		return func() {}, nil
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return func() {}, nil
	}
	var active []string
	for _, timer := range timers {
		wasActive := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", timer).Run() == nil
		wasEnabled := exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", timer).Run() == nil
		if wasActive || wasEnabled {
			active = append(active, timer)
		}
	}
	if len(active) == 0 {
		return func() {}, nil
	}
	args := append([]string{"stop"}, active...)
	if out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("stop restore timers: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return func() {
		args := append([]string{"start"}, active...)
		_ = exec.CommandContext(context.Background(), "systemctl", args...).Run()
	}, nil
}

func restorePostgres(ctx context.Context, command stackCommand, container string, user string, db string, dumpPath string) error {
	if err := dockerCompose(ctx, command, "up", "-d", container); err != nil {
		return err
	}
	ready := false
	for range 30 {
		if err := run(ctx, nil, "docker", "exec", container, "pg_isready", "-U", user); err == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		return fmt.Errorf("%s did not become ready", container)
	}
	if err := recreatePostgresDatabase(ctx, container, user, db); err != nil {
		return err
	}
	file, err := os.Open(dumpPath)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := run(ctx, file, "docker", "exec", "-i", container, "pg_restore", "--exit-on-error", "--no-owner", "--no-privileges", "-U", user, "-d", db); err != nil {
		return err
	}
	_ = dockerCompose(ctx, command, "down")
	return nil
}

func recreatePostgresDatabase(ctx context.Context, container string, user string, db string) error {
	terminate := fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid();", postgresQuoteLiteral(db))
	if err := run(ctx, nil, "docker", "exec", container, "psql", "-U", user, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", terminate); err != nil {
		return err
	}
	if err := run(ctx, nil, "docker", "exec", container, "psql", "-U", user, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", "DROP DATABASE IF EXISTS "+postgresQuoteIdentifier(db)+";"); err != nil {
		return err
	}
	if err := run(ctx, nil, "docker", "exec", container, "psql", "-U", user, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", "CREATE DATABASE "+postgresQuoteIdentifier(db)+" OWNER "+postgresQuoteIdentifier(user)+";"); err != nil {
		return err
	}
	return nil
}

func removeStoppedContainer(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", name)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	if strings.TrimSpace(string(out)) == "true" {
		return nil
	}
	return run(ctx, nil, "docker", "rm", name)
}

func postgresQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func postgresQuoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func fixGiteaDataPermissions(path string) error {
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("fix gitea data root permissions: %w", err)
	}
	if os.Geteuid() != 0 {
		return nil
	}
	for _, name := range []string{"git", "gitea"} {
		target := filepath.Join(path, name)
		if !dirExists(target) {
			continue
		}
		if err := filepath.WalkDir(target, func(item string, _ os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := os.Chown(item, 1000, 1000); err != nil {
				return fmt.Errorf("chown %s: %w", item, err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func fixOpenBaoDataPermissions(adminRoot string) error {
	if adminRoot == "" {
		return nil
	}
	path := filepath.Join(adminRoot, "data/openbao")
	if !dirExists(path) {
		return nil
	}
	if err := os.Chmod(path, 0o750); err != nil {
		return fmt.Errorf("fix openbao data root permissions: %w", err)
	}
	if os.Geteuid() != 0 {
		return nil
	}
	return filepath.WalkDir(path, func(item string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Stat(item)
		if err != nil {
			return err
		}
		if err := os.Chown(item, 100, 1000); err != nil {
			return fmt.Errorf("chown %s: %w", item, err)
		}
		mode := os.FileMode(0o600)
		if info.IsDir() {
			mode = 0o750
		}
		if err := os.Chmod(item, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", item, err)
		}
		return nil
	})
}

func restoreOpenBao(ctx context.Context, cfg config.Config, compose string, snapPath string) error {
	if err := fixOpenBaoDataPermissions(cfg.AdminRoot); err != nil {
		return err
	}
	if err := dockerCompose(ctx, stackCommand{Compose: compose}, "up", "-d"); err != nil {
		return err
	}
	status, err := waitOpenBaoStatus(ctx)
	if err != nil {
		return err
	}

	var restoreToken string
	if status.Initialized {
		if err := unsealOpenBao(ctx, cfg); err != nil {
			return err
		}
		restoreToken, err = openBaoToken(ctx, cfg)
		if err != nil {
			return err
		}
		if restoreToken == "" {
			return fmt.Errorf("openbao snapshot restore requires source recovery material")
		}
	} else {
		restoreToken, err = bootstrapOpenBaoForSnapshotRestore(ctx)
		if err != nil {
			return err
		}
	}
	if err := run(ctx, nil, "docker", "cp", snapPath, "openbao:/tmp/openbao.snap"); err != nil {
		return err
	}
	if err := run(ctx, nil, "docker", "exec", "--user", "root", "openbao", "chown", "openbao:openbao", "/tmp/openbao.snap"); err != nil {
		return err
	}
	if err := runWithEnv(ctx, nil, []string{"VAULT_TOKEN=" + restoreToken}, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN", "openbao", "bao", "operator", "raft", "snapshot", "restore", "-force", "/tmp/openbao.snap"); err != nil {
		return err
	}
	_ = dockerCompose(ctx, stackCommand{Compose: compose}, "down")
	return nil
}

type openBaoStatus struct {
	Initialized bool `json:"initialized"`
	Sealed      bool `json:"sealed"`
}

func waitOpenBaoStatus(ctx context.Context) (openBaoStatus, error) {
	var lastOutput []byte
	for range 30 {
		cmd := exec.CommandContext(ctx, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "openbao", "bao", "status", "-format=json")
		out, err := cmd.CombinedOutput()
		lastOutput = out
		if err == nil || (json.Valid(out) && strings.Contains(string(out), `"initialized"`)) {
			var status openBaoStatus
			if jsonErr := json.Unmarshal(out, &status); jsonErr == nil {
				return status, nil
			}
		}
		time.Sleep(time.Second)
	}
	return openBaoStatus{}, fmt.Errorf("openbao status unavailable: %s", strings.TrimSpace(string(lastOutput)))
}

func bootstrapOpenBaoForSnapshotRestore(ctx context.Context) (string, error) {
	out, err := openBaoRecoveryOutput(ctx, "bao", "operator", "init", "-key-shares=1", "-key-threshold=1", "-format=json")
	if err != nil {
		return "", fmt.Errorf("initialize temporary openbao recovery cluster: %w", err)
	}
	var initData struct {
		UnsealKeys []string `json:"unseal_keys_b64"`
		RootToken  string   `json:"root_token"`
	}
	if err := json.Unmarshal(out, &initData); err != nil {
		return "", fmt.Errorf("parse temporary openbao initialization: %w", err)
	}
	if len(initData.UnsealKeys) != 1 || initData.RootToken == "" {
		return "", fmt.Errorf("temporary openbao initialization returned incomplete recovery material")
	}
	if _, err := openBaoRecoveryOutput(ctx, "bao", "operator", "unseal", initData.UnsealKeys[0]); err != nil {
		return "", fmt.Errorf("unseal temporary openbao recovery cluster: %w", err)
	}
	return initData.RootToken, nil
}

func openBaoRecoveryOutput(ctx context.Context, args ...string) ([]byte, error) {
	base := []string{"exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "openbao"}
	cmd := exec.CommandContext(ctx, "docker", append(base, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker openbao recovery command failed: %w", err)
	}
	return out, nil
}

func openBaoToken(ctx context.Context, cfg config.Config) (string, error) {
	if token := strings.TrimSpace(os.Getenv("OPENBAO_TOKEN")); token != "" {
		return token, nil
	}
	for _, path := range []string{
		filepath.Join(cfg.AdminRoot, "env/openbao-restore-token"),
		filepath.Join(cfg.RepoRoot, "secrets/openbao-root-token"),
		"/opt/homelab-admin-node/secrets/openbao-root-token",
	} {
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return strings.TrimSpace(string(data)), nil
		}
	}
	token, err := openBaoTokenFromSOPS(ctx, filepath.Join(cfg.RepoRoot, "secrets/openbao-unseal.sops.yaml"))
	if err != nil {
		return "", err
	}
	return token, nil
}

func openBaoTokenFromSOPS(ctx context.Context, path string) (string, error) {
	if !fileExists(path) {
		return "", nil
	}
	cmd := exec.CommandContext(ctx, "sops", "--decrypt", "--output-type", "json", path)
	cmd.Env = append(os.Environ(), "SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("decrypt openbao token from %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	var data struct {
		OpenBao struct {
			RootToken string `json:"root_token"`
		} `json:"openbao"`
		OpenBaoConfig struct {
			RootToken string `json:"root_token"`
		} `json:"openbao_config"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return "", fmt.Errorf("parse openbao token from %s: %w", path, err)
	}
	if token := strings.TrimSpace(data.OpenBao.RootToken); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(data.OpenBaoConfig.RootToken); token != "" {
		return token, nil
	}
	return "", nil
}

func unsealOpenBao(ctx context.Context, cfg config.Config) error {
	secretsFile := filepath.Join(cfg.RepoRoot, "secrets/openbao-unseal.sops.yaml")
	return openbao.Unseal(ctx, openbao.Options{SecretsFile: secretsFile})
}

func dockerCompose(ctx context.Context, command stackCommand, args ...string) error {
	base := []string{"compose"}
	if command.EnvFile != "" && fileExists(command.EnvFile) {
		base = append(base, "--env-file", command.EnvFile)
	}
	base = append(base, "-f", command.Compose)
	base = append(base, args...)
	return run(ctx, nil, "docker", base...)
}

func run(ctx context.Context, stdin io.Reader, name string, args ...string) error {
	return runWithEnv(ctx, stdin, nil, name, args...)
}

func runWithEnv(ctx context.Context, stdin io.Reader, extraEnv []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(output.String()))
	}
	return nil
}

func restoreIDFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeMode(path string, mode string) error {
	return adminmode.Set(path, mode)
}

func inspectBackup(path, id string) (backup.Info, error) {
	backups, err := backup.List(filepath.Dir(path))
	if err != nil {
		return backup.Info{}, err
	}
	for _, item := range backups {
		if item.ID == id {
			return item, nil
		}
	}
	return backup.Info{ID: id, Path: path}, nil
}

func replaceDirContents(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("restore source is not a directory: %s", src)
	}

	if dstInfo, err := os.Stat(dst); err == nil && dstInfo.IsDir() {
		if err := clearDir(dst); err != nil {
			return err
		}
		if err := os.Chmod(dst, info.Mode()); err != nil {
			return err
		}
	} else {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return err
		}
	}

	// Archive mode preserves ownership, xattrs, ACLs, links and timestamps. On
	// Btrfs, reflinks also keep the application outage independent of data size.
	cmd := exec.Command("cp", "--archive", "--reflink=auto", "--", filepath.Clean(src)+"/.", dst+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy restored data: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func snapshotArtifactSource(root, legacyPrefix string, expected ...string) (string, error) {
	valid := func(path string) bool {
		for _, name := range expected {
			if !dirExists(filepath.Join(path, name)) {
				return false
			}
		}
		return true
	}
	if valid(root) {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var candidate string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), legacyPrefix) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !valid(path) {
			continue
		}
		if candidate != "" {
			return "", fmt.Errorf("multiple legacy snapshot payloads found")
		}
		candidate = path
	}
	if candidate == "" {
		return "", fmt.Errorf("expected directories are missing")
	}
	return candidate, nil
}

func clearDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}
