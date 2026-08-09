package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/converge"
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

func TestConvergeOptionsKeepReleaseHandoffAndDependencyContractsForEveryCaller(t *testing.T) {
	repo := t.TempDir()
	cfg := config.FromEnv()
	cfg.RepoRoot = repo
	cfg.ReleaseRefFile = filepath.Join(t.TempDir(), "release-ref")
	cfg.GitRefFile = filepath.Join(t.TempDir(), "git-ref")
	cfg.SchemaVersionFile = filepath.Join(t.TempDir(), "schema")
	cfg.PackageSnapshotFile = filepath.Join(t.TempDir(), "package-snapshot")
	cfg.PackageSnapshotModeFile = filepath.Join(t.TempDir(), "package-snapshot-mode")
	t.Setenv("ADMIN_ANSIBLE_COLLECTIONS_ROOT", filepath.Join(t.TempDir(), "collections"))
	a := app{cfg: cfg}
	opts := a.convergeOptions("inventory.ini", "site.yml", true, nil)
	for label, value := range map[string]string{
		"build script":            opts.BuildScript,
		"binary":                  opts.BinaryPath,
		"requirements":            opts.RequirementsPath,
		"collections":             opts.CollectionsRoot,
		"package snapshot source": opts.PackageSnapshotSource,
		"package snapshot state":  opts.PackageSnapshotFile,
		"package snapshot mode":   opts.PackageSnapshotModeFile,
	} {
		if value == "" {
			t.Fatalf("%s is absent from shared converge options", label)
		}
	}
}

func TestPostRestoreConvergenceHandsOffToSelectedReleaseBinary(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "selected-binary.log")
	binary := filepath.Join(dir, "admin-node")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s|%s|%s\\n' \"$*\" \"$INVENTORY_PATH\" \"$PLAYBOOK_PATH\" > \"$POST_RESTORE_TEST_LOG\"\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POST_RESTORE_TEST_LOG", logPath)
	var out, errOut bytes.Buffer
	cfg := config.FromEnv()
	cfg.RepoRoot = dir
	released := false
	reacquired := false
	a := app{
		out:    &out,
		errOut: &errOut,
		cfg:    cfg,
		converge: func(context.Context, converge.Options) error {
			return &converge.RestartRequiredError{BinaryPath: binary}
		},
	}
	opts := giteaProcessRestoreOptions{Inventory: "/config/inventory.ini"}
	if err := a.runPostRestoreConvergence(
		context.Background(),
		opts,
		func() { released = true },
		func() error { reacquired = true; return nil },
	); err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("operation lock was not released before selected binary handoff")
	}
	if !reacquired {
		t.Fatal("operation lock was not reacquired after selected binary handoff")
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"converge run --admin-checkout-aligned", "/config/inventory.ini", filepath.Join(dir, "ansible/site.yml")} {
		if !strings.Contains(string(logContent), expected) {
			t.Fatalf("selected release invocation %q does not contain %q", logContent, expected)
		}
	}
	if strings.Contains(string(logContent), "--skip-git-pull") {
		t.Fatalf("selected release invocation unexpectedly suppresses inventory update: %q", logContent)
	}
	opts.SkipGitPull = true
	if err := a.runPostRestoreConvergence(
		context.Background(),
		opts,
		func() {},
		func() error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	logContent, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logContent), "--admin-checkout-aligned --skip-git-pull") {
		t.Fatalf("explicit offline restore policy was not preserved: %q", logContent)
	}
	lockErr := errors.New("simulated contention")
	if err := a.runPostRestoreConvergence(
		context.Background(),
		opts,
		func() {},
		func() error { return lockErr },
	); !errors.Is(err, lockErr) {
		t.Fatalf("operation-lock reacquisition error = %v, want %v", err, lockErr)
	}
}

func TestVersionReportsPersistedReleaseState(t *testing.T) {
	dir := t.TempDir()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "ci@example.test"},
		{"config", "user.name", "CI"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "release"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "release/config-schema-version"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "release/arch-package-snapshot"), []byte("2026/08/08\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "release"}, {"commit", "-m", "release"}} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	revisionBytes, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.TrimSpace(string(revisionBytes))
	if output, err := exec.Command("git", "-C", repo, "checkout", "--detach").CombinedOutput(); err != nil {
		t.Fatalf("detach release: %v: %s", err, output)
	}
	paths := []struct {
		name  string
		value string
	}{
		{"release-name", revision + "\n"},
		{"release-ref", revision + "\n"},
		{"git-ref", revision + "\n"},
		{"schema", "2\n"},
		{"package-snapshot", "2026/08/08\n"},
		{"package-snapshot-mode", "qualified\n"},
		{"release-channel", "ci\n"},
	}
	for _, item := range paths {
		if err := os.WriteFile(filepath.Join(dir, item.name), []byte(item.value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.FromEnv()
	cfg.RepoRoot = repo
	cfg.ReleaseNameFile = filepath.Join(dir, "release-name")
	cfg.ReleaseRefFile = filepath.Join(dir, "release-ref")
	cfg.GitRefFile = filepath.Join(dir, "git-ref")
	cfg.SchemaVersionFile = filepath.Join(dir, "schema")
	cfg.PackageSnapshotFile = filepath.Join(dir, "package-snapshot")
	cfg.PackageSnapshotModeFile = filepath.Join(dir, "package-snapshot-mode")
	cfg.ReleaseChannelFile = filepath.Join(dir, "release-channel")
	cfg.QualificationFile = filepath.Join(dir, "qualification.json")
	var out, errOut bytes.Buffer
	a := app{out: &out, errOut: &errOut, cfg: cfg}

	if code := a.run(context.Background(), []string{"version", "--json"}); code != 0 {
		t.Fatalf("code = %d, stderr=%q", code, errOut.String())
	}
	for _, expected := range []string{`"release":"` + revision + `"`, `"config_schema":"2"`} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("version output %q does not contain %q", out.String(), expected)
		}
	}
	if output, err := exec.Command("git", "-C", repo, "checkout", "main").CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v: %s", err, output)
	}
	for path, value := range map[string]string{
		cfg.ReleaseNameFile:    "main\n",
		cfg.ReleaseRefFile:     "refs/heads/main\n",
		cfg.ReleaseChannelFile: "development\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	errOut.Reset()
	if code := a.run(context.Background(), []string{"version"}); code != 0 {
		t.Fatalf("refs/heads/main alias rejected: code=%d stderr=%q", code, errOut.String())
	}
	if output, err := exec.Command("git", "-C", repo, "checkout", "--detach").CombinedOutput(); err != nil {
		t.Fatalf("restore detached checkout: %v: %s", err, output)
	}
	for path, value := range map[string]string{
		cfg.ReleaseNameFile:    revision + "\n",
		cfg.ReleaseRefFile:     revision + "\n",
		cfg.ReleaseChannelFile: "ci\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "unqualified-local-file"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := a.run(context.Background(), []string{"version"}); code != 1 {
		t.Fatalf("dirty production checkout accepted: code=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "local changes") {
		t.Fatalf("dirty checkout diagnostic missing: %q", errOut.String())
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

func TestVersionBindsReleaseTagToInstalledRevision(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "ci@example.test"},
		{"config", "user.name", "CI"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "release"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"config-schema-version": "1\n",
		"arch-package-snapshot": "2026/08/08\n",
	} {
		if err := os.WriteFile(filepath.Join(repo, "release", name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "release"}, {"commit", "-m", "release"}, {"tag", "-a", "v1.2.3", "-m", "qualified"}, {"checkout", "--detach"}} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	revisionBytes, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.TrimSpace(string(revisionBytes))
	state := t.TempDir()
	cfg := config.FromEnv()
	cfg.RepoRoot = repo
	cfg.ReleaseNameFile = filepath.Join(state, "release-name")
	cfg.ReleaseRefFile = filepath.Join(state, "release-ref")
	cfg.GitRefFile = filepath.Join(state, "git-ref")
	cfg.SchemaVersionFile = filepath.Join(state, "schema")
	cfg.PackageSnapshotFile = filepath.Join(state, "package-snapshot")
	cfg.PackageSnapshotModeFile = filepath.Join(state, "package-snapshot-mode")
	cfg.ReleaseChannelFile = filepath.Join(state, "release-channel")
	cfg.QualificationFile = filepath.Join(state, "qualification.json")
	for path, value := range map[string]string{
		cfg.ReleaseNameFile:         "v1.2.3\n",
		cfg.ReleaseRefFile:          revision + "\n",
		cfg.GitRefFile:              revision + "\n",
		cfg.SchemaVersionFile:       "1\n",
		cfg.PackageSnapshotFile:     "2026/08/08\n",
		cfg.PackageSnapshotModeFile: "qualified\n",
		cfg.ReleaseChannelFile:      "production\n",
		cfg.QualificationFile:       `{"release":{"tag":"v1.2.3","commit":"` + revision + `"}}`,
	} {
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	a := app{out: &out, errOut: &errOut, cfg: cfg}
	if code := a.run(context.Background(), []string{"version"}); code != 0 {
		t.Fatalf("valid tag rejected: code=%d stderr=%q", code, errOut.String())
	}
	if err := os.WriteFile(cfg.ReleaseNameFile, []byte("v1.2.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := a.run(context.Background(), []string{"version"}); code != 1 {
		t.Fatalf("unbound tag accepted: code=%d", code)
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
