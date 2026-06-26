package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/backup"
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
	case "secret":
		return a.runSecret(ctx, args[1:])
	case "openbao":
		return a.runOpenBao(ctx, args[1:])
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
	fmt.Fprintln(a.out, "  secret     Manage local secret material")
	fmt.Fprintln(a.out, "  openbao    Initialize and unseal OpenBao")
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
			ID:  *restoreID,
			Out: a.out,
			Validate: func(ctx context.Context) error {
				validator := validate.NewValidator(a.cfg, a.runner)
				previous := os.Getenv("GITEA_VALIDATION_CREATE")
				os.Setenv("GITEA_VALIDATION_CREATE", "false")
				defer os.Setenv("GITEA_VALIDATION_CREATE", previous)
				results := validator.All(ctx)
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
