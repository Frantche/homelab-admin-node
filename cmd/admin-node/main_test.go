package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

func TestRootHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	a := app{out: &out, errOut: &errOut, cfg: config.FromEnv()}

	code := a.run(context.Background(), nil)

	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "Usage: admin-node") {
		t.Fatalf("help output missing usage: %q", out.String())
	}
	if !strings.Contains(out.String(), "process environment, then /srv/admin/env/backup.env") {
		t.Fatalf("help output missing runtime precedence: %q", out.String())
	}
}

func TestOperationalCommandReportsRuntimeConfigurationErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	a := app{out: &out, errOut: &errOut, configErr: errors.New("malformed managed file")}

	code := a.run(context.Background(), []string{"validate", "all"})

	if code != 1 || !strings.Contains(errOut.String(), "runtime configuration: malformed managed file") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestGiteaRestoreRefusesRuntimeConfigurationErrorsBeforeDestructiveWork(t *testing.T) {
	var out, errOut bytes.Buffer
	a := app{out: &out, errOut: &errOut, configErr: errors.New("malformed managed file")}

	code := a.run(context.Background(), []string{"gitea", "restore-process", "--backup-filename", "gitea-backup-2026-08-08-12-00-00.zip"})

	if code != 1 || !strings.Contains(errOut.String(), "runtime configuration: malformed managed file") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestOperationalCommandRejectsBuiltInExampleDomains(t *testing.T) {
	var out, errOut bytes.Buffer
	a := app{out: &out, errOut: &errOut, cfg: config.Config{
		KeycloakDomain: config.DefaultKeycloakDomain,
		HarborDomain:   config.DefaultHarborDomain,
		GiteaDomain:    config.DefaultGiteaDomain,
		TraefikDomain:  config.DefaultTraefikDomain,
		OpenBaoDomain:  config.DefaultOpenBaoDomain,
	}}

	code := a.run(context.Background(), []string{"validate", "all"})

	if code != 1 || !strings.Contains(errOut.String(), "built-in example values") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestManualAndSystemdEquivalentCommandsResolveSameManagedConfig(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(filepath.Join(backupRoot, "20260808-120000"), 0o700); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(root, "backup.env")
	managed := "ADMIN_BACKUP_ROOT=" + backupRoot + `
KEYCLOAK_DOMAIN=keycloak.deployed.test
HARBOR_DOMAIN=harbor.deployed.test
GITEA_DOMAIN=gitea.deployed.test
TRAEFIK_DOMAIN=traefik.deployed.test
OPENBAO_DOMAIN=bao.deployed.test
`
	if err := os.WriteFile(envFile, []byte(managed), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ADMIN_BACKUP_ROOT", "KEYCLOAK_DOMAIN", "HARBOR_DOMAIN", "GITEA_DOMAIN", "TRAEFIK_DOMAIN", "OPENBAO_DOMAIN"} {
		t.Setenv(key, "")
	}
	t.Setenv("RESTIC_BACKUP_ENV_FILE", envFile)
	manualConfig, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	var manualOut, manualErr bytes.Buffer
	manualApp := app{out: &manualOut, errOut: &manualErr, cfg: manualConfig}
	if code := manualApp.run(context.Background(), []string{"backup", "list"}); code != 0 {
		t.Fatalf("manual command code=%d stderr=%q", code, manualErr.String())
	}

	t.Setenv("RESTIC_BACKUP_ENV_FILE", filepath.Join(root, "missing.env"))
	t.Setenv("ADMIN_BACKUP_ROOT", backupRoot)
	t.Setenv("KEYCLOAK_DOMAIN", "keycloak.deployed.test")
	t.Setenv("HARBOR_DOMAIN", "harbor.deployed.test")
	t.Setenv("GITEA_DOMAIN", "gitea.deployed.test")
	t.Setenv("TRAEFIK_DOMAIN", "traefik.deployed.test")
	t.Setenv("OPENBAO_DOMAIN", "bao.deployed.test")
	systemdConfig, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	var systemdOut, systemdErr bytes.Buffer
	systemdApp := app{out: &systemdOut, errOut: &systemdErr, cfg: systemdConfig}
	if code := systemdApp.run(context.Background(), []string{"backup", "list"}); code != 0 {
		t.Fatalf("systemd-equivalent command code=%d stderr=%q", code, systemdErr.String())
	}

	if manualOut.String() != systemdOut.String() || !strings.Contains(manualOut.String(), "20260808-120000") {
		t.Fatalf("manual output %q differs from systemd-equivalent output %q", manualOut.String(), systemdOut.String())
	}
}

func TestReadEnvFileDecodesQuotedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "process.env")
	content := "DOUBLE=\"pa\\\"ss$word\\\\tail\"\nSINGLE='literal $value'\nPLAIN=unchanged\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	values, err := readEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := values["DOUBLE"]; got != "pa\"ss$word\\tail" {
		t.Fatalf("DOUBLE = %q", got)
	}
	if got := values["SINGLE"]; got != "literal $value" {
		t.Fatalf("SINGLE = %q", got)
	}
	if got := values["PLAIN"]; got != "unchanged" {
		t.Fatalf("PLAIN = %q", got)
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	a := app{out: &out, errOut: &errOut, cfg: config.FromEnv()}

	code := a.run(context.Background(), []string{"nope"})

	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("error output missing unknown command: %q", errOut.String())
	}
}

func TestVersionReportsPersistedReleaseState(t *testing.T) {
	dir := t.TempDir()
	paths := []struct {
		name  string
		value string
	}{
		{"release-name", "v1.2.3\n"},
		{"release-ref", strings.Repeat("a", 40) + "\n"},
		{"git-ref", strings.Repeat("a", 40) + "\n"},
		{"schema", "2\n"},
	}
	for _, item := range paths {
		if err := os.WriteFile(filepath.Join(dir, item.name), []byte(item.value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.FromEnv()
	cfg.ReleaseNameFile = filepath.Join(dir, "release-name")
	cfg.ReleaseRefFile = filepath.Join(dir, "release-ref")
	cfg.GitRefFile = filepath.Join(dir, "git-ref")
	cfg.SchemaVersionFile = filepath.Join(dir, "schema")
	var out, errOut bytes.Buffer
	a := app{out: &out, errOut: &errOut, cfg: cfg}

	if code := a.run(context.Background(), []string{"version", "--json"}); code != 0 {
		t.Fatalf("code = %d, stderr=%q", code, errOut.String())
	}
	for _, expected := range []string{`"release":"v1.2.3"`, `"config_schema":"2"`} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("version output %q does not contain %q", out.String(), expected)
		}
	}
}

func TestVersionFailsWhenPersistedReleaseStateIsIncompleteOrMismatched(t *testing.T) {
	for _, test := range []struct {
		name     string
		pin      string
		revision string
		missing  bool
	}{
		{name: "missing state", missing: true},
		{name: "mismatched production revision", pin: strings.Repeat("a", 40), revision: strings.Repeat("b", 40)},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.FromEnv()
			cfg.ReleaseNameFile = filepath.Join(dir, "release-name")
			cfg.ReleaseRefFile = filepath.Join(dir, "release-ref")
			cfg.GitRefFile = filepath.Join(dir, "git-ref")
			cfg.SchemaVersionFile = filepath.Join(dir, "schema")
			if !test.missing {
				for path, value := range map[string]string{
					cfg.ReleaseNameFile:   "v1.2.3\n",
					cfg.ReleaseRefFile:    test.pin + "\n",
					cfg.GitRefFile:        test.revision + "\n",
					cfg.SchemaVersionFile: "1\n",
				} {
					if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			var out, errOut bytes.Buffer
			a := app{out: &out, errOut: &errOut, cfg: cfg}
			if code := a.run(context.Background(), []string{"version", "--json"}); code != 1 {
				t.Fatalf("code = %d, want 1; stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if !strings.Contains(errOut.String(), "version:") {
				t.Fatalf("missing diagnostic: %q", errOut.String())
			}
		})
	}
}

func TestSubcommandsExist(t *testing.T) {
	tests := [][]string{
		{"backup", "list"},
		{"validate", "harbor"},
		{"validate", "openbao"},
		{"test", "harbor-scanner"},
		{"ci", "create-sentinel"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			cfg := config.FromEnv()
			cfg.AdminRoot = t.TempDir()
			cfg.BackupRoot = t.TempDir()
			cfg.ValidateMockAll = true
			a := app{out: &out, errOut: &errOut, cfg: cfg}

			code := a.run(context.Background(), args)

			if code != 0 {
				t.Fatalf("code = %d, want 0, stderr=%q", code, errOut.String())
			}
			if out.Len() == 0 {
				t.Fatal("expected output")
			}
		})
	}
}
