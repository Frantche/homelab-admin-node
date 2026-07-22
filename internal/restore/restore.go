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
	"strings"
	"syscall"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/backup"
	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/openbao"
)

type Options struct {
	ID            string
	Validate      func(context.Context) error
	Out           io.Writer
	LockFile      string
	SystemdTimers []string
}

func Run(ctx context.Context, cfg config.Config, opts Options) error {
	unlock, err := acquireLock(opts.LockFile)
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
	defer resumeTimers()

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

	set := stackSet(cfg.AdminRoot)
	stopStacks(ctx, cfg, set)
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

	if dirExists(filepath.Join(info.Path, "gitea-data")) {
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

	if fileExists(filepath.Join(info.Path, "keycloak.dump")) {
		if err := restorePostgres(ctx, stackCommand{Compose: set.KeycloakCompose, EnvFile: set.KeycloakEnv}, "keycloak-db", "keycloak", "keycloak", filepath.Join(info.Path, "keycloak.dump")); err != nil {
			writeMode(cfg.ModeFile, "restore_failed")
			return fmt.Errorf("restore keycloak: %w", err)
		}
	}
	if fileExists(filepath.Join(info.Path, "gitea.dump")) && fileExists(set.GiteaCompose) && fileExists(set.GiteaEnv) {
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
	return nil
}

func Resolve(root, id string) (backup.Info, bool, error) {
	if id == "latest" {
		return backup.Latest(root)
	}
	path := filepath.Join(root, id)
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

func stopStacks(ctx context.Context, cfg config.Config, set stacks) {
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
			_ = dockerCompose(ctx, command, "down")
		}
	}
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

func acquireLock(path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
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
		if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", timer).Run() == nil {
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
	if err := waitOpenBaoInitialized(ctx); err != nil {
		return err
	}
	if err := unsealOpenBao(ctx, cfg); err != nil {
		return err
	}
	if err := run(ctx, nil, "docker", "cp", snapPath, "openbao:/tmp/openbao.snap"); err != nil {
		return err
	}
	if err := run(ctx, nil, "docker", "exec", "--user", "root", "openbao", "chown", "openbao:openbao", "/tmp/openbao.snap"); err != nil {
		return err
	}
	token, err := openBaoToken(ctx, cfg)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("openbao snapshot restore requires OPENBAO_TOKEN or secrets/openbao-root-token")
	}
	if err := run(ctx, nil, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN="+token, "openbao", "bao", "operator", "raft", "snapshot", "restore", "-force", "/tmp/openbao.snap"); err != nil {
		return err
	}
	_ = dockerCompose(ctx, stackCommand{Compose: compose}, "down")
	return nil
}

func openBaoToken(ctx context.Context, cfg config.Config) (string, error) {
	if token := strings.TrimSpace(os.Getenv("OPENBAO_TOKEN")); token != "" {
		return token, nil
	}
	for _, path := range []string{
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

func waitOpenBaoInitialized(ctx context.Context) error {
	var lastOutput []byte
	for range 30 {
		cmd := exec.CommandContext(ctx, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "openbao", "bao", "status")
		out, err := cmd.CombinedOutput()
		lastOutput = out
		if err == nil || strings.Contains(string(out), "Initialized") {
			if strings.Contains(string(out), "Initialized") {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("openbao did not become initialized: %s", strings.TrimSpace(string(lastOutput)))
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
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(output.String()))
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(mode+"\n"), 0o644)
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

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return copyFile(src, dst, info.Mode())
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

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
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

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}
