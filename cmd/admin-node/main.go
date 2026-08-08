package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/backup"
	"github.com/Frantche/homelab-admin-node/internal/citest"
	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/converge"
	"github.com/Frantche/homelab-admin-node/internal/mode"
	"github.com/Frantche/homelab-admin-node/internal/openbao"
	"github.com/Frantche/homelab-admin-node/internal/operation"
	"github.com/Frantche/homelab-admin-node/internal/restore"
	"github.com/Frantche/homelab-admin-node/internal/runner"
	"github.com/Frantche/homelab-admin-node/internal/secret"
	"github.com/Frantche/homelab-admin-node/internal/validate"
)

var giteaProcessBackupFilenamePattern = regexp.MustCompile(`^gitea-backup-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{2}-[0-9]{2}-[0-9]{2}\.zip$`)

type app struct {
	out    io.Writer
	errOut io.Writer
	cfg    config.Config
	runner runner.Runner
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := app{
		out:    os.Stdout,
		errOut: os.Stderr,
		cfg:    config.FromEnv(),
		runner: runner.ExecRunner{},
	}
	os.Exit(a.run(ctx, os.Args[1:]))
}

func (a app) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		a.printRootUsage()
		return 0
	}

	switch args[0] {
	case "-h", "--help", "help":
		a.printRootUsage()
		return 0
	case "validate":
		return a.runValidate(ctx, args[1:])
	case "test":
		return a.runTest(ctx, args[1:])
	case "backup":
		return a.runBackup(ctx, args[1:])
	case "restore":
		return a.runRestore(ctx, args[1:])
	case "mode":
		return a.runMode(ctx, args[1:])
	case "converge":
		return a.runConverge(ctx, args[1:])
	case "gitea":
		return a.runGitea(ctx, args[1:])
	case "secret":
		return a.runSecret(ctx, args[1:])
	case "openbao":
		return a.runOpenBao(ctx, args[1:])
	case "ci":
		return a.runCI(ctx, args[1:])
	default:
		fmt.Fprintf(a.errOut, "unknown command: %s\n\n", args[0])
		a.printRootUsage()
		return 2
	}
}

func (a app) printRootUsage() {
	fmt.Fprintln(a.out, "Usage: admin-node <command> [options]")
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, "Commands:")
	fmt.Fprintln(a.out, "  validate   Validate admin-node services")
	fmt.Fprintln(a.out, "  test       Run explicit state-changing service tests")
	fmt.Fprintln(a.out, "  backup     Manage backups")
	fmt.Fprintln(a.out, "  restore    Restore backups")
	fmt.Fprintln(a.out, "  mode       Manage admin-node mode")
	fmt.Fprintln(a.out, "  converge   Run Ansible convergence")
	fmt.Fprintln(a.out, "  gitea      Manage Gitea operations")
	fmt.Fprintln(a.out, "  secret     Manage local secret material")
	fmt.Fprintln(a.out, "  openbao    Initialize and unseal OpenBao")
	fmt.Fprintln(a.out, "  ci         Run CI helper operations")
}

func (a app) runCI(ctx context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "")
	fs := flag.NewFlagSet("ci", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	rootTokenOut := fs.String("root-token-out", getenv("OPENBAO_ROOT_TOKEN_OUT", ""), "OpenBao CI root token output path")
	keysetName := fs.String("keyset-name", getenv("KEYSET_NAME", "ci-keyset"), "OpenBao CI keyset name")
	sentinelPath := fs.String("sentinel-path", getenv("ADMIN_NODE_SENTINEL_PATH", ""), "sentinel data file path")
	configPath := fs.String("config-path", getenv("OPENBAO_CONFIG_PATH", ""), "config repo group_vars/all.yml path")
	token := fs.String("token", getenv("OPENBAO_TOKEN", ""), "OpenBao root token")
	tokenFile := fs.String("token-file", getenv("OPENBAO_TOKEN_FILE", ""), "OpenBao root token file")
	ageKey := fs.String("age-key", getenv("SOPS_AGE_KEY_FILE", ""), "SOPS age private key")
	mockSource := fs.String("source", getenv("CI_MOCK_CONFIG_SOURCE", ""), "mock config repo source directory")
	mockDest := fs.String("dest", getenv("CONFIG_REPO_DIR", ""), "mock config repo destination directory")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	var err error
	switch subcommand {
	case "init-openbao":
		err = citest.InitOpenBao(ctx, a.cfg, citest.OpenBaoOptions{RootTokenOut: *rootTokenOut, KeysetName: *keysetName})
		if err == nil {
			fmt.Fprintln(a.out, "CI OpenBao initialized")
		}
	case "create-sentinel":
		err = citest.CreateSentinel(a.cfg, *sentinelPath)
		if err == nil {
			fmt.Fprintln(a.out, "CI sentinel data created")
		}
	case "install-mock-config-repo":
		err = citest.InstallMockConfigRepo(ctx, a.cfg, *mockSource, *mockDest)
		if err == nil {
			fmt.Fprintln(a.out, "CI mock config repo installed")
		}
	case "update-openbao-token":
		err = citest.UpdateOpenBaoToken(*configPath, *token, *tokenFile, *ageKey)
		if err == nil {
			fmt.Fprintln(a.out, "CI OpenBao token updated")
		}
	default:
		fmt.Fprintf(a.errOut, "unknown ci command: %s\n", subcommand)
		return 2
	}
	if err != nil {
		fmt.Fprintf(a.errOut, "ci %s: %v\n", subcommand, err)
		return 1
	}
	return 0
}

func (a app) runMode(_ context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "")
	switch subcommand {
	case "set":
		if len(rest) != 1 {
			fmt.Fprintln(a.errOut, "usage: admin-node mode set <locked|init|normal|restore|restore_failed>")
			return 2
		}
		if err := mode.Set(a.cfg.ModeFile, rest[0]); err != nil {
			fmt.Fprintf(a.errOut, "mode set: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.out, "Mode set to %s\n", rest[0])
		return 0
	case "check":
		fs := flag.NewFlagSet("mode check", flag.ContinueOnError)
		fs.SetOutput(a.errOut)
		allowed := fs.String("allow", "", "comma-separated allowed modes")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		current, err := mode.Read(a.cfg.ModeFile)
		if err != nil {
			fmt.Fprintf(a.errOut, "mode check: %v\n", err)
			return 1
		}
		for _, candidate := range strings.Split(*allowed, ",") {
			if strings.TrimSpace(candidate) == current {
				return 0
			}
		}
		fmt.Fprintf(a.errOut, "mode %s is not allowed\n", current)
		return 1
	default:
		fmt.Fprintln(a.errOut, "usage: admin-node mode <set|check>")
		return 2
	}
}

func (a app) runConverge(ctx context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "run")
	if subcommand != "run" {
		fmt.Fprintf(a.errOut, "unknown converge command: %s\n", subcommand)
		return 2
	}
	fs := flag.NewFlagSet("converge", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	skipGitPull := fs.Bool("skip-git-pull", envBool("ADMIN_CONVERGE_SKIP_GIT_PULL"), "skip git pull before convergence")
	extraVars := fs.String("extra-vars", "", "extra ansible-playbook arguments")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	unlock, err := operation.Acquire(a.cfg.OperationLock)
	if err != nil {
		fmt.Fprintf(a.errOut, "converge run: %v\n", err)
		return 1
	}
	defer unlock()
	extraArgs := converge.SplitExtraArgs(os.Getenv("ANSIBLE_EXTRA_ARGS"))
	if *extraVars != "" {
		extraArgs = append(extraArgs, converge.SplitExtraArgs(*extraVars)...)
	}
	fmt.Fprintln(a.out, "[admin-converge] starting")
	playbook := getenv("PLAYBOOK_PATH", a.cfg.RepoRoot+"/ansible/site.yml")
	inventory := getenv("INVENTORY_PATH", "/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini")
	fmt.Fprintf(a.out, "[admin-converge] playbook=%s inventory=%s\n", playbook, inventory)
	if err := converge.Run(ctx, converge.Options{
		RepoDir:       a.cfg.RepoRoot,
		InventoryPath: inventory,
		PlaybookPath:  playbook,
		SkipGitPull:   *skipGitPull,
		ExtraArgs:     extraArgs,
	}); err != nil {
		fmt.Fprintf(a.errOut, "converge run: %v\n", err)
		return 1
	}
	return 0
}

func (a app) runGitea(ctx context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "")
	if subcommand != "restore-process" {
		fmt.Fprintf(a.errOut, "unknown gitea command: %s\n", subcommand)
		return 2
	}
	fs := flag.NewFlagSet("gitea restore-process", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	backupFilename := fs.String("backup-filename", getenv("BACKUP_FILENAME", ""), "exact gitea-backup-restore-process archive filename")
	processEnv := fs.String("process-env", getenv("GITEA_PROCESS_BACKUP_ENV", "/srv/admin/env/gitea-process-backup.env"), "gitea process backup environment file")
	giteaEnv := fs.String("gitea-env", getenv("GITEA_ENV", "/srv/admin/env/gitea.env"), "Gitea compose environment file")
	giteaCompose := fs.String("gitea-compose", getenv("GITEA_COMPOSE", "/srv/admin/stacks/gitea/compose.yaml"), "Gitea compose file")
	preRestoreDir := fs.String("pre-restore-dir", getenv("GITEA_PROCESS_PRE_RESTORE_DIR", "/srv/admin/backups/pre-gitea-process-restore"), "local safety copy directory")
	runConverge := fs.Bool("converge", envBoolDefault("GITEA_PROCESS_RESTORE_CONVERGE", true), "run normal convergence after restore")
	inventory := fs.String("inventory", getenv("INVENTORY_PATH", "/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini"), "inventory path used for post-restore convergence")
	skipGitPull := fs.Bool("skip-git-pull", envBool("ADMIN_CONVERGE_SKIP_GIT_PULL"), "skip git pull before post-restore convergence")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if *backupFilename == "" {
		fmt.Fprintln(a.errOut, "usage: admin-node gitea restore-process --backup-filename <gitea-backup-YYYY-MM-DD-HH-MM-SS.zip>")
		return 2
	}
	if err := a.runGiteaProcessRestore(ctx, giteaProcessRestoreOptions{
		BackupFilename: *backupFilename,
		ProcessEnv:     *processEnv,
		GiteaEnv:       *giteaEnv,
		GiteaCompose:   *giteaCompose,
		PreRestoreDir:  *preRestoreDir,
		RunConverge:    *runConverge,
		Inventory:      *inventory,
		SkipGitPull:    *skipGitPull,
	}); err != nil {
		fmt.Fprintf(a.errOut, "gitea restore-process: %v\n", err)
		return 1
	}
	return 0
}

type giteaProcessRestoreOptions struct {
	BackupFilename string
	ProcessEnv     string
	GiteaEnv       string
	GiteaCompose   string
	PreRestoreDir  string
	RunConverge    bool
	Inventory      string
	SkipGitPull    bool
}

func (a app) runGiteaProcessRestore(ctx context.Context, opts giteaProcessRestoreOptions) error {
	unlock, err := operation.Acquire(a.cfg.OperationLock)
	if err != nil {
		return fmt.Errorf("acquire operation lock: %w", err)
	}
	defer unlock()
	if err := validateGiteaProcessRestoreInputs(a.cfg.GiteaStackPath, opts); err != nil {
		return err
	}

	env, err := readEnvFile(opts.ProcessEnv)
	if err != nil {
		return err
	}
	image := envValue(env, "GITEA_PROCESS_BACKUP_IMAGE", "ghcr.io/frantche/gitea-backup-restore-process:0.3.21@sha256:fa400b039e740d1a84d3bb9af76a1d69276f1abfaf995db06d10dc813954ec52")
	databaseNetwork := envValue(env, "GITEA_PROCESS_BACKUP_NETWORK", "gitea-db")
	egressNetwork := envValue(env, "GITEA_PROCESS_BACKUP_EGRESS_NETWORK", "admin-edge")
	restoreTmp := envValue(env, "RESTORE_TMP_FOLDER", "/srv/admin/backups/gitea-process/restore-tmp")
	backupFileLog := envValue(env, "BACKUP_FILE_LOG", "/srv/admin/backups/gitea-process/history/backupFileLog.txt")

	fmt.Fprintf(a.out, "[gitea-restore-process] restoring %s\n", opts.BackupFilename)
	if err := mode.Set(a.cfg.ModeFile, "restore"); err != nil {
		return fmt.Errorf("set restore mode: %w", err)
	}

	restoreComplete := false
	defer func() {
		if !restoreComplete {
			_ = mode.Set(a.cfg.ModeFile, "restore_failed")
		}
	}()

	resumeTimers, err := restore.SuspendSystemdTimers(ctx, []string{
		"admin-converge.timer",
		"admin-backup.timer",
		"admin-gitea-process-backup.timer",
	})
	if err != nil {
		return fmt.Errorf("suspend timers: %w", err)
	}

	restoreDockerCommand := []string{"docker", "run", "--rm"}
	restoreDockerCommand = append(restoreDockerCommand, giteaProcessNetworkArgs(databaseNetwork, egressNetwork)...)
	writableMountArgs, err := giteaProcessWritableMountArgs(restoreTmp, backupFileLog)
	if err != nil {
		return err
	}
	restoreDockerCommand = append(restoreDockerCommand,
		"--env-file", opts.ProcessEnv,
		"-e", "BACKUP_FILENAME="+opts.BackupFilename,
		"-v", filepath.Join(a.cfg.GiteaStackPath, "gitea")+":/data",
	)
	restoreDockerCommand = append(restoreDockerCommand, writableMountArgs...)
	restoreDockerCommand = append(restoreDockerCommand, image, "gitea-restore")

	commands := [][]string{
		{"docker", "compose", "--env-file", opts.GiteaEnv, "-f", opts.GiteaCompose, "up", "-d", "gitea-db"},
		{"docker", "compose", "--env-file", opts.GiteaEnv, "-f", opts.GiteaCompose, "stop", "gitea"},
		{"install", "-d", "-m", "0700", opts.PreRestoreDir},
	}
	for _, command := range commands {
		if err := a.execLogged(ctx, command[0], command[1:]...); err != nil {
			return err
		}
	}
	if err := a.backupGiteaProcessDatabase(ctx, filepath.Join(opts.PreRestoreDir, "gitea.dump")); err != nil {
		return err
	}
	commands = [][]string{
		{"rsync", "-a", "--delete", filepath.Join(a.cfg.GiteaStackPath, "gitea") + "/", filepath.Join(opts.PreRestoreDir, "gitea-data") + "/"},
		{"find", filepath.Join(a.cfg.GiteaStackPath, "gitea/git/repositories"), "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "{}", "+"},
		{"find", filepath.Join(a.cfg.GiteaStackPath, "gitea/gitea/avatars"), "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "{}", "+"},
		{"find", filepath.Join(a.cfg.GiteaStackPath, "gitea/gitea/repo-avatars"), "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "{}", "+"},
		{"install", "-d", "-m", "0700", restoreTmp},
		{"find", restoreTmp, "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "{}", "+"},
		restoreDockerCommand,
	}
	for _, command := range commands {
		if err := a.execLogged(ctx, command[0], command[1:]...); err != nil {
			return err
		}
	}
	if err := a.restoreGiteaProcessDatabase(ctx, filepath.Join(restoreTmp, "dump.postgres.sql")); err != nil {
		return err
	}
	if err := a.normalizeGiteaProcessRestorePermissions(ctx); err != nil {
		return err
	}
	if err := a.execLogged(ctx, "docker", "compose", "--env-file", opts.GiteaEnv, "-f", opts.GiteaCompose, "up", "-d"); err != nil {
		return err
	}

	validationCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	results := giteaProcessRestoreValidation(validationCtx, a.runner)
	validate.WriteText(a.out, results)
	if validate.HasFailure(results) {
		return fmt.Errorf("restored Gitea validation failed")
	}

	if err := mode.Set(a.cfg.ModeFile, "normal"); err != nil {
		return fmt.Errorf("set normal mode: %w", err)
	}

	if opts.RunConverge {
		if err := converge.Run(ctx, converge.Options{
			RepoDir:       a.cfg.RepoRoot,
			InventoryPath: opts.Inventory,
			PlaybookPath:  getenv("PLAYBOOK_PATH", a.cfg.RepoRoot+"/ansible/site.yml"),
			SkipGitPull:   opts.SkipGitPull,
		}); err != nil {
			return fmt.Errorf("post-restore convergence: %w", err)
		}
	}

	restoreComplete = true
	if err := resumeTimers(); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "[gitea-restore-process] restore validated, mode set to normal, and timers resumed")
	return nil
}

func validateGiteaProcessRestoreInputs(giteaStackPath string, opts giteaProcessRestoreOptions) error {
	if !giteaProcessBackupFilenamePattern.MatchString(opts.BackupFilename) {
		return fmt.Errorf("backup filename must match gitea-backup-YYYY-MM-DD-HH-MM-SS.zip")
	}
	preRestoreDir := filepath.Clean(opts.PreRestoreDir)
	if !filepath.IsAbs(preRestoreDir) || preRestoreDir == "/" {
		return fmt.Errorf("pre-restore safety copy directory must be an absolute bounded path")
	}
	stackPath := filepath.Clean(giteaStackPath)
	if !filepath.IsAbs(stackPath) || stackPath == "/" {
		return fmt.Errorf("Gitea stack path must be an absolute bounded path")
	}
	relative, err := filepath.Rel(stackPath, preRestoreDir)
	if err != nil {
		return fmt.Errorf("compare Gitea restore paths: %w", err)
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))) {
		return fmt.Errorf("pre-restore safety copy directory must not be inside Gitea data")
	}
	for _, marker := range []string{"gitea.dump", "gitea-data"} {
		path := filepath.Join(preRestoreDir, marker)
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("pre-restore safety copy already exists at %s; archive it and select an empty directory before retrying", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect pre-restore safety copy %s: %w", path, err)
		}
	}
	return nil
}

func giteaProcessNetworkArgs(databaseNetwork, egressNetwork string) []string {
	args := []string{"--network", databaseNetwork}
	if egressNetwork != databaseNetwork {
		args = append(args, "--network", egressNetwork)
	}
	return args
}

func giteaProcessWritableMountArgs(restoreTmp, backupFileLog string) ([]string, error) {
	if !filepath.IsAbs(restoreTmp) || !filepath.IsAbs(backupFileLog) {
		return nil, fmt.Errorf("Gitea process writable paths must be absolute")
	}
	historyDir := filepath.Dir(backupFileLog)
	if restoreTmp == "/" || historyDir == "/" {
		return nil, fmt.Errorf("refusing to mount filesystem root for Gitea process writable paths")
	}
	args := []string{"-v", restoreTmp + ":" + restoreTmp}
	if historyDir != restoreTmp {
		args = append(args, "-v", historyDir+":"+historyDir)
	}
	return args, nil
}

func (a app) restoreGiteaProcessDatabase(ctx context.Context, dumpPath string) error {
	if _, err := os.Stat(dumpPath); err != nil {
		return fmt.Errorf("gitea process dump not found: %w", err)
	}
	if err := a.execLogged(ctx, "docker", "exec", "-i", "gitea-db", "psql", "-U", "gitea", "-d", "gitea", "-v", "ON_ERROR_STOP=1", "-c", "DROP SCHEMA public CASCADE; CREATE SCHEMA public AUTHORIZATION gitea; GRANT ALL ON SCHEMA public TO gitea; GRANT ALL ON SCHEMA public TO public;"); err != nil {
		return fmt.Errorf("reset gitea database schema: %w", err)
	}
	script := `sed -e 's/OWNER TO app/OWNER TO gitea/g' "$1" | docker exec -i gitea-db psql -U gitea -d gitea -v ON_ERROR_STOP=1`
	if err := a.execLogged(ctx, "sh", "-c", script, "gitea-process-db-restore", dumpPath); err != nil {
		return fmt.Errorf("restore gitea process database dump: %w", err)
	}
	return nil
}

func (a app) backupGiteaProcessDatabase(ctx context.Context, dumpPath string) error {
	if !filepath.IsAbs(dumpPath) || filepath.Clean(dumpPath) == "/" {
		return fmt.Errorf("pre-restore Gitea database dump path must be an absolute file path")
	}
	tmpPath := dumpPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create pre-restore Gitea database dump: %w", err)
	}
	cmd := exec.CommandContext(ctx, "docker", "exec", "gitea-db", "pg_dump", "--format=custom", "--no-owner", "--no-privileges", "-U", "gitea", "-d", "gitea")
	cmd.Stdout = file
	cmd.Stderr = a.errOut
	runErr := cmd.Run()
	closeErr := file.Close()
	if runErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("create pre-restore Gitea database dump: %w", runErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close pre-restore Gitea database dump: %w", closeErr)
	}
	if info, err := os.Stat(tmpPath); err != nil || info.Size() == 0 {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("pre-restore Gitea database dump is empty")
	}
	if err := os.Rename(tmpPath, dumpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("publish pre-restore Gitea database dump: %w", err)
	}
	return nil
}

func (a app) normalizeGiteaProcessRestorePermissions(ctx context.Context) error {
	paths := []string{
		filepath.Join(a.cfg.GiteaStackPath, "gitea/git"),
		filepath.Join(a.cfg.GiteaStackPath, "gitea/gitea/avatars"),
		filepath.Join(a.cfg.GiteaStackPath, "gitea/gitea/repo-avatars"),
		filepath.Join(a.cfg.GiteaStackPath, "gitea/gitea/attachments"),
		filepath.Join(a.cfg.GiteaStackPath, "gitea/gitea/packages"),
		filepath.Join(a.cfg.GiteaStackPath, "gitea/gitea/repo-archive"),
	}
	existing := existingPaths(paths)
	if len(existing) == 0 {
		return fmt.Errorf("no gitea restore paths found under %s", filepath.Join(a.cfg.GiteaStackPath, "gitea"))
	}
	if err := a.execLogged(ctx, "chown", append([]string{"-R", "1000:1000"}, existing...)...); err != nil {
		return fmt.Errorf("normalize gitea restore ownership: %w", err)
	}
	findArgs := append([]string{}, existing...)
	findArgs = append(findArgs, "-type", "d", "-exec", "chmod", "0700", "{}", "+")
	if err := a.execLogged(ctx, "find", findArgs...); err != nil {
		return fmt.Errorf("normalize gitea restore directory modes: %w", err)
	}
	findArgs = append([]string{}, existing...)
	findArgs = append(findArgs, "-type", "f", "-exec", "chmod", "u+rw,go-rwx", "{}", "+")
	if err := a.execLogged(ctx, "find", findArgs...); err != nil {
		return fmt.Errorf("normalize gitea restore file modes: %w", err)
	}
	return nil
}

func existingPaths(paths []string) []string {
	var existing []string
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			existing = append(existing, path)
		}
	}
	return existing
}

func (a app) execLogged(ctx context.Context, name string, args ...string) error {
	fmt.Fprintf(a.out, "[gitea-restore-process] running %s %s\n", name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = a.out
	cmd.Stderr = a.errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func readEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read process env: %w", err)
	}
	defer file.Close()
	values := map[string]string{}
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
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			if decoded, err := strconv.Unquote(value); err == nil {
				value = decoded
			}
		} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = value[1 : len(value)-1]
		}
		values[strings.TrimSpace(key)] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read process env: %w", err)
	}
	return values, nil
}

func envValue(values map[string]string, key, fallback string) string {
	if value := values[key]; value != "" {
		return value
	}
	return fallback
}

func (a app) runSecret(_ context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "")
	if subcommand != "install-age-key" || len(rest) != 1 {
		fmt.Fprintln(a.errOut, "usage: admin-node secret install-age-key <path>")
		return 2
	}
	dst := getenv("SOPS_AGE_KEY_FILE", "/etc/sops/age/keys.txt")
	if err := secret.InstallAgeKey(rest[0], dst); err != nil {
		fmt.Fprintf(a.errOut, "install age key: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.out, "Age key installed at %s with 0400 permissions\n", dst)
	return 0
}

func (a app) runOpenBao(ctx context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "")
	fs := flag.NewFlagSet("openbao", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	ageKey := fs.String("age-key", getenv("AGE_KEY", "/etc/sops/age/keys.txt"), "SOPS age private key")
	secretsDir := fs.String("secrets-dir", getenv("SECRETS_DIR", a.cfg.RepoRoot+"/secrets"), "secrets directory")
	secretsFile := fs.String("secrets-file", getenv("SECRETS_FILE", ""), "OpenBao encrypted unseal secrets file")
	keysetName := fs.String("keyset-name", getenv("KEYSET_NAME", ""), "OpenBao keyset name")
	container := fs.String("container", getenv("OPENBAO_CONTAINER", "openbao"), "OpenBao container name")
	rootTokenOut := fs.String("root-token-out", getenv("OPENBAO_ROOT_TOKEN_OUT", ""), "optional root token output path")
	token := fs.String("token", getenv("OPENBAO_TOKEN", ""), "OpenBao root token")
	tokenFile := fs.String("token-file", getenv("OPENBAO_TOKEN_FILE", ""), "OpenBao root token file")
	kvPath := fs.String("path", getenv("OPENBAO_KV_PATH", "secret"), "OpenBao KV engine path")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	opts := openbao.Options{
		AgeKey:        *ageKey,
		SecretsDir:    *secretsDir,
		SecretsFile:   *secretsFile,
		KeysetName:    *keysetName,
		Container:     *container,
		RootTokenOut:  *rootTokenOut,
		RootToken:     *token,
		RootTokenFile: *tokenFile,
	}
	var err error
	switch subcommand {
	case "init-if-needed":
		err = openbao.InitIfNeeded(ctx, opts)
	case "unseal":
		err = openbao.Unseal(ctx, opts)
	case "enable-kv":
		err = openbao.EnableKV(ctx, opts, *kvPath)
	default:
		fmt.Fprintf(a.errOut, "unknown openbao command: %s\n", subcommand)
		return 2
	}
	if err != nil {
		fmt.Fprintf(a.errOut, "openbao %s: %v\n", subcommand, err)
		return 1
	}
	return 0
}

func (a app) runBackup(ctx context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "run")
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	includeImages := fs.Bool("include-images", false, "include Docker images, rendered stack definitions, and repository bundle in the backup")
	verifyID := fs.String("id", "latest", "backup id to verify")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	switch subcommand {
	case "run":
		info, err := backup.Run(ctx, a.cfg, backup.RunOptions{
			IncludeImages: *includeImages,
			Validate: func(ctx context.Context) error {
				validator := validate.NewValidator(a.cfg, a.runner)
				results := validator.All(ctx)
				validate.WriteText(a.out, results)
				if validate.HasFailure(results) {
					return fmt.Errorf("validation failed")
				}
				return nil
			},
		})
		if err != nil {
			fmt.Fprintf(a.errOut, "backup run: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.out, "Backup completed: %s\n", info.Path)
		return 0
	case "list":
		backups, err := backup.List(a.cfg.BackupRoot)
		if err != nil {
			fmt.Fprintf(a.errOut, "list backups: %v\n", err)
			return 1
		}
		writer := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tCREATED\tSIZE\tMANIFEST\tDUMPS\tOFFLINE_IMAGES")
		for _, item := range backups {
			manifest := "missing"
			if item.HasManifest {
				manifest = "ok"
			}
			if item.ManifestInvalid {
				manifest = "invalid"
			}
			dumps := formatDumps(item)
			offline := "no"
			if item.HasOfflineImage {
				offline = "yes"
			}
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.CreatedAt.Format(time.RFC3339), backup.FormatSize(item.SizeBytes), manifest, dumps, offline)
		}
		writer.Flush()
		return 0
	case "verify":
		id := *verifyID
		info, ok, err := restore.Resolve(a.cfg.BackupRoot, id)
		if err != nil || !ok {
			if err == nil {
				err = fmt.Errorf("backup not found")
			}
			fmt.Fprintf(a.errOut, "backup verify: %v\n", err)
			return 1
		}
		manifest, err := backup.Verify(info.Path)
		if err != nil {
			fmt.Fprintf(a.errOut, "backup verify: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.out, "Backup verified: %s (%d files, %s)\n", manifest.ID, len(manifest.Files), manifest.Consistency)
		return 0
	case "offline-status":
		status, err := backup.CheckOfflineStatus(a.cfg, time.Now().UTC())
		if err != nil {
			fmt.Fprintf(a.errOut, "backup offline-status: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.out, "offline_id=%s age=%s fresh=%t verified=%t recovery_kit_complete=%t recovery_kit_checked=%s\n", status.ID, status.Age.Round(time.Second), status.Fresh, status.Verified, status.RecoveryKitComplete, status.RecoveryKitChecked.Format(time.RFC3339))
		for _, problem := range status.Problems {
			fmt.Fprintf(a.errOut, "offline recovery: %s\n", problem)
		}
		if len(status.Problems) > 0 {
			return 1
		}
		return 0
	case "restic":
		paths := fs.Args()
		if err := backup.RunRestic(ctx, a.cfg.BackupEnvFile, paths); err != nil {
			fmt.Fprintf(a.errOut, "backup restic: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(a.errOut, "unknown backup command: %s\n", subcommand)
		return 2
	}
}

func formatDumps(item backup.Info) string {
	var names []string
	if item.HasKeycloakDump {
		names = append(names, "keycloak")
	}
	if item.HasGiteaDump {
		names = append(names, "gitea")
	}
	if item.HasHarborDump {
		names = append(names, "harbor")
	}
	if item.HasOpenBaoSnap {
		names = append(names, "openbao")
	}
	if item.HasGiteaData {
		names = append(names, "gitea-data")
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ",")
}

func (a app) runRestore(ctx context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "run")
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	restoreID := fs.String("id", "", "backup id to restore (defaults to /etc/admin-node/restore-id, then latest)")
	repositoryID := fs.String("repository", "", "restic repository to fetch from when the backup is not local")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	switch subcommand {
	case "run":
		if *repositoryID != "" {
			if *restoreID == "" || *restoreID == "latest" {
				fmt.Fprintln(a.errOut, "restore run: --repository requires an explicit --id")
				return 2
			}
			if _, err := os.Stat(filepath.Join(a.cfg.BackupRoot, *restoreID)); os.IsNotExist(err) {
				if err := backup.RestoreFromRestic(ctx, a.cfg.BackupEnvFile, *repositoryID, a.cfg.BackupRoot, *restoreID); err != nil {
					fmt.Fprintf(a.errOut, "restore fetch: %v\n", err)
					return 1
				}
			}
		}
		err := restore.Run(ctx, a.cfg, restore.Options{
			ID:                    *restoreID,
			Out:                   a.out,
			LockFile:              a.cfg.OperationLock,
			RestoreKeycloakAdmin:  func(ctx context.Context) error { return restore.RestoreKeycloakAdmin(ctx, a.cfg) },
			RestoreHarborWritable: func(ctx context.Context) error { return restore.RestoreHarborWritable(ctx, a.cfg) },
			SystemdTimers: []string{
				"admin-converge.timer",
				"admin-backup.timer",
				"admin-gitea-process-backup.timer",
			},
			Validate: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
				defer cancel()
				results := restoreValidation(ctx, a.runner)
				validate.WriteText(a.out, results)
				if validate.HasFailure(results) {
					return fmt.Errorf("restore validation failed")
				}
				return nil
			},
		})
		if err != nil {
			fmt.Fprintf(a.errOut, "restore run: %v\n", err)
			return 1
		}
		return 0
	case "select":
		backups, err := backup.List(a.cfg.BackupRoot)
		if err != nil {
			fmt.Fprintf(a.errOut, "list backups: %v\n", err)
			return 1
		}
		id, err := restore.Select(os.Stdin, a.out, backups)
		if err != nil {
			fmt.Fprintf(a.errOut, "restore select: %v\n", err)
			return 1
		}
		fmt.Fprintln(a.out, id)
		return 0
	default:
		fmt.Fprintf(a.errOut, "unknown restore command: %s\n", subcommand)
		return 2
	}
}

func splitSubcommand(args []string, fallback string) (string, []string) {
	if len(args) == 0 {
		return fallback, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return fallback, args
	}
	return args[0], args[1:]
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string) bool {
	return strings.EqualFold(os.Getenv(key), "true")
}

func envBoolDefault(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return strings.EqualFold(value, "true")
}
