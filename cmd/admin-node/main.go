package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/backup"
	"github.com/Frantche/homelab-admin-node/internal/citest"
	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/converge"
	"github.com/Frantche/homelab-admin-node/internal/mode"
	"github.com/Frantche/homelab-admin-node/internal/openbao"
	"github.com/Frantche/homelab-admin-node/internal/restore"
	"github.com/Frantche/homelab-admin-node/internal/runner"
	"github.com/Frantche/homelab-admin-node/internal/secret"
	"github.com/Frantche/homelab-admin-node/internal/validate"
)

type app struct {
	out    io.Writer
	errOut io.Writer
	cfg    config.Config
	runner runner.Runner
}

func main() {
	a := app{
		out:    os.Stdout,
		errOut: os.Stderr,
		cfg:    config.FromEnv(),
		runner: runner.ExecRunner{},
	}
	os.Exit(a.run(context.Background(), os.Args[1:]))
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
		err = citest.InstallMockConfigRepo(a.cfg, *mockSource, *mockDest)
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

func (a app) runValidate(_ context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "all")
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	output := fs.String("output", "text", "output format: text or json")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	validator := validate.NewValidator(a.cfg, a.runner)
	ctx := context.Background()
	var results []validate.CheckResult
	switch subcommand {
	case "all":
		results = validator.All(ctx)
	case "apis":
		results = validator.APIS(ctx)
	case "harbor":
		results = []validate.CheckResult{validator.Harbor(ctx)}
	case "openbao":
		results = []validate.CheckResult{validator.OpenBao(ctx)}
	case "gitea":
		results = []validate.CheckResult{validator.Gitea(ctx)}
	case "dns":
		results = []validate.CheckResult{validator.DNS(ctx)}
	case "tunnel":
		results = []validate.CheckResult{validator.Tunnel(ctx)}
	case "hardening":
		results = []validate.CheckResult{validator.Hardening(ctx)}
	case "observability":
		results = []validate.CheckResult{validator.Observability(ctx)}
	default:
		fmt.Fprintf(a.errOut, "unknown validate command: %s\n", subcommand)
		return 2
	}

	switch *output {
	case "text":
		validate.WriteText(a.out, results)
	case "json":
		if err := validate.WriteJSON(a.out, results); err != nil {
			fmt.Fprintf(a.errOut, "write json output: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(a.errOut, "unknown output format: %s\n", *output)
		return 2
	}

	if validate.HasFailure(results) {
		return 1
	}
	return 0
}

func (a app) runMode(_ context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "")
	if subcommand != "set" || len(rest) != 1 {
		fmt.Fprintln(a.errOut, "usage: admin-node mode set <locked|init|normal|restore|restore_failed>")
		return 2
	}
	if err := mode.Set(a.cfg.ModeFile, rest[0]); err != nil {
		fmt.Fprintf(a.errOut, "mode set: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.out, "Mode set to %s\n", rest[0])
	return 0
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
	env, err := readEnvFile(opts.ProcessEnv)
	if err != nil {
		return err
	}
	image := envValue(env, "GITEA_PROCESS_BACKUP_IMAGE", "ghcr.io/frantche/gitea-backup-restore-process:0.3.6")
	network := envValue(env, "GITEA_PROCESS_BACKUP_NETWORK", "admin-net")
	restoreTmp := envValue(env, "RESTORE_TMP_FOLDER", "/srv/admin/backups/gitea-process/restore-tmp")

	fmt.Fprintf(a.out, "[gitea-restore-process] restoring %s\n", opts.BackupFilename)
	if err := mode.Set(a.cfg.ModeFile, "locked"); err != nil {
		return fmt.Errorf("set locked mode: %w", err)
	}

	restoreComplete := false
	defer func() {
		if !restoreComplete {
			_ = mode.Set(a.cfg.ModeFile, "locked")
		}
	}()

	if err := a.execLogged(ctx, "systemctl", "stop", "admin-gitea-process-backup.timer"); err != nil {
		fmt.Fprintf(a.errOut, "[gitea-restore-process] warning: %v\n", err)
	}

	commands := [][]string{
		{"docker", "compose", "--env-file", opts.GiteaEnv, "-f", opts.GiteaCompose, "up", "-d", "gitea-db"},
		{"docker", "compose", "--env-file", opts.GiteaEnv, "-f", opts.GiteaCompose, "stop", "gitea"},
		{"install", "-d", "-m", "0700", opts.PreRestoreDir},
		{"rsync", "-a", "--delete", filepath.Join(a.cfg.AdminRoot, "data/gitea") + "/", filepath.Join(opts.PreRestoreDir, "gitea-data") + "/"},
		{"find", filepath.Join(a.cfg.AdminRoot, "data/gitea/git/repositories"), "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "{}", "+"},
		{"find", filepath.Join(a.cfg.AdminRoot, "data/gitea/gitea/avatars"), "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "{}", "+"},
		{"find", filepath.Join(a.cfg.AdminRoot, "data/gitea/gitea/repo-avatars"), "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "{}", "+"},
		{"install", "-d", "-m", "0700", restoreTmp},
		{"find", restoreTmp, "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "{}", "+"},
		{"docker", "run", "--rm",
			"--network", network,
			"--env-file", opts.ProcessEnv,
			"-e", "BACKUP_FILENAME=" + opts.BackupFilename,
			"-v", filepath.Join(a.cfg.AdminRoot, "data/gitea") + ":/data",
			"-v", restoreTmp + ":" + restoreTmp,
			image,
			"gitea-restore"},
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

	restoreComplete = true
	if err := mode.Set(a.cfg.ModeFile, "normal"); err != nil {
		return fmt.Errorf("set normal mode: %w", err)
	}
	fmt.Fprintln(a.out, "[gitea-restore-process] restore completed and mode set to normal")

	if !opts.RunConverge {
		return nil
	}
	return converge.Run(ctx, converge.Options{
		RepoDir:       a.cfg.RepoRoot,
		InventoryPath: opts.Inventory,
		PlaybookPath:  getenv("PLAYBOOK_PATH", a.cfg.RepoRoot+"/ansible/site.yml"),
		SkipGitPull:   opts.SkipGitPull,
	})
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

func (a app) normalizeGiteaProcessRestorePermissions(ctx context.Context) error {
	paths := []string{
		filepath.Join(a.cfg.AdminRoot, "data/gitea/git"),
		filepath.Join(a.cfg.AdminRoot, "data/gitea/gitea/avatars"),
		filepath.Join(a.cfg.AdminRoot, "data/gitea/gitea/repo-avatars"),
		filepath.Join(a.cfg.AdminRoot, "data/gitea/gitea/attachments"),
		filepath.Join(a.cfg.AdminRoot, "data/gitea/gitea/packages"),
		filepath.Join(a.cfg.AdminRoot, "data/gitea/gitea/repo-archive"),
	}
	existing := existingPaths(paths)
	if len(existing) == 0 {
		return fmt.Errorf("no gitea restore paths found under %s", filepath.Join(a.cfg.AdminRoot, "data/gitea"))
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
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
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
	includeImages := fs.Bool("include-images", false, "include Docker images in the backup")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	switch subcommand {
	case "run":
		info, err := backup.Run(context.Background(), a.cfg, backup.RunOptions{
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

func (a app) runRestore(_ context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "run")
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	restoreID := fs.String("id", "latest", "backup id to restore")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	switch subcommand {
	case "run":
		err := restore.Run(context.Background(), a.cfg, restore.Options{
			ID:       *restoreID,
			Out:      a.out,
			LockFile: "/run/admin-converge.lock",
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

func restoreValidation(ctx context.Context, run runner.Runner) []validate.CheckResult {
	return []validate.CheckResult{
		restoreDockerHealth(ctx, run, "OpenBao", "openbao"),
		restoreDockerHealth(ctx, run, "Keycloak", "keycloak"),
		restoreDockerHealth(ctx, run, "Harbor", "harbor-core"),
		restoreDockerHealth(ctx, run, "Gitea", "gitea"),
		restoreDockerHealth(ctx, run, "Traefik", "traefik"),
		restoreCommandCheck(ctx, run, "OpenBao", 90*time.Second, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "openbao", "sh", "-c", "bao status -format=json | grep '\"sealed\": false' >/dev/null"),
		restoreCommandCheck(ctx, run, "Keycloak", 120*time.Second, "docker", "exec", "keycloak", "bash", "-lc", "timeout 3 bash -c 'exec 3<>/dev/tcp/127.0.0.1/9000; printf \"GET /health/ready HTTP/1.1\\r\\nHost: localhost\\r\\nConnection: close\\r\\n\\r\\n\" >&3; head -1 <&3 | grep -q \"200\"'"),
		restoreCommandCheck(ctx, run, "Harbor", 120*time.Second, "docker", "exec", "harbor-core", "curl", "-fsS", "http://127.0.0.1:8080/api/v2.0/health"),
		restoreCommandCheck(ctx, run, "Gitea", 120*time.Second, "docker", "exec", "gitea", "curl", "-fsS", "http://127.0.0.1:3000/api/v1/version"),
		restoreCommandCheck(ctx, run, "Traefik", 90*time.Second, "docker", "exec", "traefik", "traefik", "healthcheck", "--ping"),
	}
}

func restoreDockerHealth(ctx context.Context, run runner.Runner, name, container string) validate.CheckResult {
	return restoreWait(ctx, name, 120*time.Second, func(ctx context.Context) (validate.Status, string, bool) {
		result := run.Run(ctx, "docker", "inspect", "-f", "{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", container)
		if result.Code != 0 {
			return validate.StatusFail, container + " is unavailable", false
		}
		fields := strings.Fields(result.Stdout)
		if len(fields) < 2 || fields[0] != "true" {
			return validate.StatusFail, container + " is not running", false
		}
		if fields[1] == "healthy" || fields[1] == "none" {
			return validate.StatusOK, container + " running " + fields[1], true
		}
		return validate.StatusFail, container + " health is " + fields[1], false
	})
}

func restoreCommandCheck(ctx context.Context, run runner.Runner, name string, timeout time.Duration, command string, args ...string) validate.CheckResult {
	return restoreWait(ctx, name, timeout, func(ctx context.Context) (validate.Status, string, bool) {
		result := run.Run(ctx, command, args...)
		if result.Code == 0 {
			return validate.StatusOK, "internal endpoint reachable", true
		}
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = strings.TrimSpace(result.Stdout)
		}
		if message == "" {
			message = "internal endpoint unavailable"
		}
		return validate.StatusFail, message, false
	})
}

func restoreWait(ctx context.Context, name string, timeout time.Duration, check func(context.Context) (validate.Status, string, bool)) validate.CheckResult {
	start := time.Now()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var status validate.Status
	var message string
	var ok bool
	for {
		status, message, ok = check(waitCtx)
		if ok {
			break
		}
		if waitCtx.Err() != nil {
			break
		}
		time.Sleep(3 * time.Second)
	}
	return validate.CheckResult{Name: name, Status: status, Message: message, Duration: time.Since(start)}
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
