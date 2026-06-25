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
	"github.com/Frantche/homelab-admin-node/internal/restore"
	"github.com/Frantche/homelab-admin-node/internal/runner"
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
	case "gitea":
		results = []validate.CheckResult{validator.Gitea(ctx)}
	case "dns":
		results = []validate.CheckResult{validator.DNS(ctx)}
	case "tunnel":
		results = []validate.CheckResult{validator.Tunnel(ctx)}
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

func (a app) runBackup(_ context.Context, args []string) int {
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
