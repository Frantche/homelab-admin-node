package backup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

func TestRunRefusesLockedMode(t *testing.T) {
	root := t.TempDir()
	modeFile := filepath.Join(root, "mode")
	if err := os.WriteFile(modeFile, []byte("locked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), config.Config{ModeFile: modeFile, BackupRoot: filepath.Join(root, "backups")}, RunOptions{})
	if err == nil {
		t.Fatal("expected locked mode error")
	}
}

func TestRunCreatesBackupWithManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeDocker := filepath.Join(binDir, "docker")
	if err := os.WriteFile(fakeDocker, []byte(`#!/usr/bin/env bash
set -euo pipefail
if [[ "$1 $2" == "ps --format" ]]; then
  echo "gitea-db"
  echo "harbor-db"
  exit 0
fi
if [[ "${1:-} ${2:-} ${3:-} ${4:-}" == "exec keycloak-db pg_dump -Fc" ]]; then
  echo "keycloak-dump"
  exit 0
fi
if [[ "${1:-} ${2:-} ${3:-} ${4:-}" == "exec gitea-db pg_dump -Fc" ]]; then
  echo "gitea-dump"
  exit 0
fi
if [[ "${1:-} ${2:-} ${3:-} ${4:-}" == "exec harbor-db pg_dump -Fc" ]]; then
  echo "harbor-dump"
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"operator raft snapshot save"* ]]; then
  printf 'openbao-snap' > "$OPENBAO_SCRATCH_TEST_PATH"
  exit 0
fi
if [[ "${1:-}" == "save" ]]; then
  echo "images" > "$3"
  exit 0
fi
if [[ "${1:-} ${2:-} ${3:-}" == "image inspect --format" ]]; then
  echo "sha256:offline-image-id"
  exit 0
fi
if [[ "${1:-}" == "tag" || "${1:-} ${2:-}" == "image rm" ]]; then
  exit 0
fi
echo "unexpected docker args: $*" >&2
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENBAO_TOKEN", "token")

	modeFile := filepath.Join(root, "mode")
	if err := os.WriteFile(modeFile, []byte("normal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adminRoot := filepath.Join(root, "admin")
	for _, dir := range []string{"stacks", "env", "data/gitea", "data/harbor/registry", "backups/openbao-scratch"} {
		if err := os.MkdirAll(filepath.Join(adminRoot, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scratchPath := filepath.Join(adminRoot, "backups/openbao-scratch/openbao.snap")
	t.Setenv("OPENBAO_SCRATCH_TEST_PATH", scratchPath)
	if err := os.WriteFile(filepath.Join(adminRoot, "stacks/compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gitea", "harbor", "keycloak", "openbao"} {
		if err := os.MkdirAll(filepath.Join(adminRoot, "stacks", name), 0o755); err != nil {
			t.Fatal(err)
		}
		compose := "services: {}\n"
		if name == "gitea" {
			compose = "services:\n  gitea:\n    image: docker.gitea.com/gitea:1.26.4\n"
		}
		if err := os.WriteFile(filepath.Join(adminRoot, "stacks", name, "compose.yaml"), []byte(compose), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(adminRoot, "stacks/cloudflared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "stacks/cloudflared/compose.yaml"), []byte("services:\n  tunnel:\n    image: cloudflare/cloudflared:missing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "env/service.env"), []byte("A=B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "data/gitea/app.ini"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "data/harbor/registry/blob"), []byte("registry-data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "Backup Test"},
		{"config", "user.email", "backup-test@example.invalid"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "tracked"), []byte("revision\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "tracked"}, {"commit", "-qm", "test revision"}} {
		if output, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}

	resticLog := filepath.Join(root, "restic.log")
	fakeRestic := filepath.Join(binDir, "restic")
	if err := os.WriteFile(fakeRestic, []byte(`#!/usr/bin/env bash
set -euo pipefail
echo "$*" >> "`+resticLog+`"
if [[ "$*" == "cat config" ]]; then
  exit 1
fi
if [[ "${1:-}" == "snapshots" ]]; then
  printf '[{"id":"verified-snapshot"}]\n'
fi
exit 0
`), 0o755); err != nil {
		t.Fatal(err)
	}
	backupEnv := filepath.Join(root, "backup.env")
	if err := os.WriteFile(backupEnv, []byte(`RESTIC_REPOSITORY="s3:https://example.invalid/bucket"
RESTIC_PASSWORD="secret"
RESTIC_INIT_REPOSITORIES="true"
RESTIC_DEFAULT_FORGET_ARGS="--keep-last 2 --prune"
BACKUP_REQUIRE_REMOTE_REPOSITORY="true"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	validateCalled := false

	cfg := config.Config{
		AdminRoot:              adminRoot,
		RepoRoot:               repoRoot,
		ModeFile:               modeFile,
		BackupRoot:             filepath.Join(root, "backups"),
		BackupEnvFile:          backupEnv,
		CIMockCloudflareTunnel: true,
	}
	info, err := Run(context.Background(), cfg, RunOptions{
		Validate: func(context.Context) error {
			validateCalled = true
			return nil
		},
		Now: func() time.Time {
			return time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
		},
		IncludeImages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !validateCalled {
		t.Fatal("validation was not called")
	}
	if info.ID != "20260625-120000" {
		t.Fatalf("ID = %q", info.ID)
	}
	for _, name := range []string{"keycloak.dump", "gitea.dump", "harbor.dump", "openbao.snap", "gitea-data", "offline-images.tar", "stack-definitions", "repository.bundle", ManifestName} {
		if !fileExists(filepath.Join(info.Path, name)) && !dirExists(filepath.Join(info.Path, name)) {
			t.Fatalf("expected %s in backup", name)
		}
	}
	snapshot, err := os.ReadFile(filepath.Join(info.Path, "openbao.snap"))
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot) != "openbao-snap" {
		t.Fatalf("openbao snapshot = %q", snapshot)
	}
	snapshotInfo, err := os.Stat(filepath.Join(info.Path, "openbao.snap"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshotInfo.Mode().Perm() != 0o600 {
		t.Fatalf("openbao snapshot mode = %#o, want 0600", snapshotInfo.Mode().Perm())
	}
	if _, err := os.Stat(scratchPath); !os.IsNotExist(err) {
		t.Fatalf("openbao scratch was not removed: %v", err)
	}
	resticCalls, err := os.ReadFile(resticLog)
	if err != nil {
		t.Fatal(err)
	}
	if string(resticCalls) == "" || !strings.Contains(string(resticCalls), "backup") {
		t.Fatalf("restic was not called correctly: %q", string(resticCalls))
	}
	manifest, ok, err := ReadManifest(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || manifest.ID != "20260625-120000" || !manifest.OfflineImages || !manifest.StackDefinitions || !manifest.RepositoryBundle || len(manifest.Images) != 1 {
		t.Fatalf("manifest = %#v ok=%t", manifest, ok)
	}
	if len(manifest.OfflineImageArchives) != 1 || manifest.OfflineImageArchives[0].ImageID != "sha256:offline-image-id" {
		t.Fatalf("offline image archive manifest = %#v", manifest.OfflineImageArchives)
	}
	if strings.Join(manifest.ActiveStacks, ",") != "gitea,harbor,keycloak,openbao" {
		t.Fatalf("active stacks = %#v", manifest.ActiveStacks)
	}
	if len(manifest.Artifacts) == 0 {
		t.Fatal("manifest artifact inventory is empty")
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Required && artifact.Status != ArtifactProduced {
			t.Fatalf("required artifact is not produced: %#v", artifact)
		}
	}
	if manifest.Consistency != "service-specific-consistency-boundaries" || len(manifest.ConsistencyBoundaries) != 1 {
		t.Fatalf("consistency contract = %#v / %#v", manifest.Consistency, manifest.ConsistencyBoundaries)
	}
	if boundary := manifest.ConsistencyBoundaries[0]; boundary.Service != "gitea" || boundary.Method != "application-already-stopped-postgresql-dump-and-filesystem-copy" {
		t.Fatalf("Gitea consistency boundary = %#v", boundary)
	}
}

func TestRunFailsWhenActiveOpenBaoSnapshotTokenIsMissing(t *testing.T) {
	root := t.TempDir()
	adminRoot := filepath.Join(root, "admin")
	writeActiveStack(t, adminRoot, "openbao")
	t.Setenv("OPENBAO_TOKEN", "")

	_, err := Run(context.Background(), config.Config{
		AdminRoot:     adminRoot,
		RepoRoot:      filepath.Join(root, "repo-without-token"),
		ModeFile:      writeBackupMode(t, root),
		BackupRoot:    filepath.Join(root, "backups"),
		BackupEnvFile: writeRequiredRemoteConfig(t, root),
	}, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "snapshot token") {
		t.Fatalf("error = %v, want missing OpenBao snapshot token", err)
	}
	failureManifestPath := filepath.Join(root, "backups", ".failed")
	failureIDs, readErr := os.ReadDir(failureManifestPath)
	if readErr != nil || len(failureIDs) != 1 {
		t.Fatalf("failed artifact inventory was not retained: entries=%v error=%v", failureIDs, readErr)
	}
	failureManifest, ok, readErr := ReadManifest(filepath.Join(failureManifestPath, failureIDs[0].Name()))
	if readErr != nil || !ok || failureManifest.Complete {
		t.Fatalf("invalid failed artifact inventory: manifest=%#v ok=%t error=%v", failureManifest, ok, readErr)
	}
	foundOpenBaoFailure := false
	for _, artifact := range failureManifest.Artifacts {
		if artifact.Path == "openbao.snap" && artifact.Status == ArtifactFailed {
			foundOpenBaoFailure = true
		}
	}
	if !foundOpenBaoFailure {
		t.Fatalf("OpenBao failure is absent from artifact inventory: %#v", failureManifest.Artifacts)
	}
}

func TestRunFailsWhenActiveGiteaDatabaseIsUnavailable(t *testing.T) {
	root := t.TempDir()
	adminRoot := filepath.Join(root, "admin")
	writeActiveStack(t, adminRoot, "gitea")
	if err := os.MkdirAll(filepath.Join(adminRoot, "data/gitea"), 0o700); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := Run(context.Background(), config.Config{
		AdminRoot:     adminRoot,
		ModeFile:      writeBackupMode(t, root),
		BackupRoot:    filepath.Join(root, "backups"),
		BackupEnvFile: writeRequiredRemoteConfig(t, root),
	}, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "gitea-db is not running") {
		t.Fatalf("error = %v, want unavailable Gitea database", err)
	}
}

func writeActiveStack(t *testing.T, adminRoot, name string) {
	t.Helper()
	path := filepath.Join(adminRoot, "stacks", name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeBackupMode(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "mode")
	if err := os.WriteFile(path, []byte("normal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRequiredRemoteConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "backup.env")
	content := "BACKUP_REQUIRE_REMOTE_REPOSITORY=true\n" +
		"RESTIC_REPOSITORIES=offsite\n" +
		"RESTIC_REPOSITORY_OFFSITE=sftp:backup:/srv/restic\n" +
		"RESTIC_PASSWORD_OFFSITE=test-password\n" +
		"RESTIC_FORGET_ARGS_OFFSITE=none\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunStandardBackupIncludesActiveStackDefinitions(t *testing.T) {
	root := t.TempDir()
	adminRoot := filepath.Join(root, "admin")
	stackDir := filepath.Join(adminRoot, "stacks", "custom")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	modeFile := filepath.Join(root, "mode")
	if err := os.WriteFile(modeFile, []byte("normal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "restic"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"snapshots\" ]]; then printf '[{\"id\":\"test-snapshot\"}]\\n'; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	info, err := Run(context.Background(), config.Config{
		AdminRoot:     adminRoot,
		ModeFile:      modeFile,
		BackupRoot:    filepath.Join(root, "backups"),
		BackupEnvFile: writeRequiredRemoteConfig(t, root),
	}, RunOptions{
		Now: func() time.Time {
			return time.Date(2026, 6, 25, 13, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Verify(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.StackDefinitions || strings.Join(manifest.ActiveStacks, ",") != "custom" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if !fileExists(filepath.Join(info.Path, "stack-definitions/custom/compose.yaml")) {
		t.Fatal("active stack definition is missing")
	}
	if fileExists(filepath.Join(info.Path, "offline-images.tar")) || fileExists(filepath.Join(info.Path, "repository.bundle")) {
		t.Fatal("standard backup unexpectedly contains offline-only artifacts")
	}
}

func TestHarborAdminCredentialsUseOnlyExplicitManagedKeys(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	if err := os.WriteFile(envFile, []byte(`RESTIC_PASSWORD="unrelated-secret"
HARBOR_ADMIN_USER="managed-admin"
HARBOR_ADMIN_PASSWORD='managed-password'
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARBOR_ADMIN_USER", "")
	t.Setenv("HARBOR_ADMIN_PASSWORD", "")

	user, password := harborAdminCredentials(config.Config{BackupEnvFile: envFile})
	if user != "managed-admin" || password != "managed-password" {
		t.Fatalf("credentials = %q/%q", user, password)
	}
}

func TestHarborAdminCredentialsPreferProcessEnvironment(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	if err := os.WriteFile(envFile, []byte("HARBOR_ADMIN_USER=managed-admin\nHARBOR_ADMIN_PASSWORD=managed-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARBOR_ADMIN_USER", "explicit-admin")
	t.Setenv("HARBOR_ADMIN_PASSWORD", "explicit-password")

	user, password := harborAdminCredentials(config.Config{BackupEnvFile: envFile})
	if user != "explicit-admin" || password != "explicit-password" {
		t.Fatalf("credentials = %q/%q", user, password)
	}
}

func TestCopyActiveStackDefinitionsPreservesRenderedModes(t *testing.T) {
	root := t.TempDir()
	adminRoot := filepath.Join(root, "admin")
	fixtures := map[string]os.FileMode{
		"harbor/config/registry.yml":            0o644,
		"openbao/openbao.hcl":                   0o644,
		"observability/otel-collector.yaml":     0o644,
		"observability/private-rendered.secret": 0o600,
	}
	for rel, mode := range fixtures {
		path := filepath.Join(adminRoot, "stacks", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("rendered\n"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"harbor", "observability", "openbao"} {
		compose := filepath.Join(adminRoot, "stacks", name, "compose.yaml")
		if err := os.WriteFile(compose, []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(compose, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	partial := filepath.Join(root, "backup")
	if err := copyActiveStackDefinitions(adminRoot, partial, []string{"harbor", "observability", "openbao"}); err != nil {
		t.Fatal(err)
	}
	for rel, want := range fixtures {
		assertBackupMode(t, filepath.Join(partial, "stack-definitions", rel), want)
	}
	assertBackupMode(t, filepath.Join(partial, "stack-definitions/harbor/config"), 0o755)
}

func TestCopyPathStillRestrictsNonStackArtifacts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "artifact"), []byte("sensitive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := copyPath(source, target); err != nil {
		t.Fatal(err)
	}
	assertBackupMode(t, target, 0o700)
	assertBackupMode(t, filepath.Join(target, "artifact"), 0o600)
}

func TestBackupGiteaQuiescesWritesAcrossDumpAndFilesystemCopy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	source := filepath.Join(root, "gitea-stack")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(source, "state")
	if err := os.WriteFile(statePath, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "docker.log")
	installGiteaBoundaryDocker(t, root, logPath, statePath)

	times := []time.Time{
		time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 8, 10, 0, 2, 0, time.UTC),
	}
	nextTime := func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}
	target := filepath.Join(root, "backup")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	boundary, err := backupGitea(context.Background(), config.Config{
		GiteaStackPath:            source,
		SnapshotRoot:              filepath.Join(root, "snapshots"),
		GiteaBackupQuiesceTimeout: time.Minute,
	}, target, "20260808-100000", nextTime)
	if err != nil {
		t.Fatal(err)
	}
	if boundary.Service != "gitea" || boundary.Method != "application-quiesced-postgresql-dump-and-filesystem-copy" {
		t.Fatalf("boundary = %#v", boundary)
	}
	if !boundary.StartedAt.Equal(time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)) || !boundary.CompletedAt.Equal(time.Date(2026, 8, 8, 10, 0, 2, 0, time.UTC)) {
		t.Fatalf("boundary timestamps = %#v", boundary)
	}
	backupState, err := os.ReadFile(filepath.Join(target, "gitea-stack/state"))
	if err != nil {
		t.Fatal(err)
	}
	if string(backupState) != "before\n" {
		t.Fatalf("backup state = %q, want pre-restart state", backupState)
	}
	liveState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(liveState) != "after\n" {
		t.Fatalf("live state = %q, want post-restart write", liveState)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	stopAt := strings.Index(logText, "stop --time 60 gitea")
	dumpAt := strings.Index(logText, "exec gitea-db pg_dump")
	startAt := strings.Index(logText, "start gitea")
	if stopAt < 0 || dumpAt <= stopAt || startAt <= dumpAt {
		t.Fatalf("unexpected consistency command order:\n%s", logText)
	}
}

func TestBackupGiteaAttemptsRestartAfterBoundaryFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	for _, test := range []struct {
		name          string
		failStop      bool
		failStart     bool
		invalidSource bool
	}{
		{name: "enter boundary", failStop: true},
		{name: "capture filesystem", invalidSource: true},
		{name: "leave boundary", failStart: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "gitea-stack")
			if err := os.MkdirAll(source, 0o755); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(source, "state")
			if err := os.WriteFile(statePath, []byte("before\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if test.invalidSource {
				if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(source, "escape")); err != nil {
					t.Fatal(err)
				}
			}
			logPath := filepath.Join(root, "docker.log")
			installGiteaBoundaryDocker(t, root, logPath, statePath)
			t.Setenv("FAIL_GITEA_STOP", strconv.FormatBool(test.failStop))
			t.Setenv("FAIL_GITEA_START", strconv.FormatBool(test.failStart))
			target := filepath.Join(root, "backup")
			if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatal(err)
			}
			_, err := backupGitea(context.Background(), config.Config{
				GiteaStackPath:            source,
				SnapshotRoot:              filepath.Join(root, "snapshots"),
				GiteaBackupQuiesceTimeout: time.Minute,
			}, target, "20260808-100000", time.Now)
			if err == nil {
				t.Fatal("expected injected boundary failure")
			}
			logData, readErr := os.ReadFile(logPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(logData), "start gitea") {
				t.Fatalf("Gitea restart was not attempted after failure:\n%s", logData)
			}
		})
	}
}

func TestVerifyRejectsUnprovenGiteaConsistencyClaim(t *testing.T) {
	root := t.TempDir()
	backupID := "20260808-100000"
	backupDir := filepath.Join(root, backupID)
	if err := os.MkdirAll(filepath.Join(backupDir, "stack-definitions", "gitea"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "stack-definitions", "gitea", "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := BuildManifestFiles(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 8, 10, 1, 0, 0, time.UTC)
	manifest := Manifest{
		Version: ManifestVersion, ID: backupID, CreatedAt: createdAt,
		ActiveStacks: []string{"gitea"}, StackDefinitions: true,
		Consistency: "logical-online", Complete: true, Files: files,
	}
	if err := WriteManifest(backupDir, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(backupDir); err == nil || !strings.Contains(err.Error(), "requires a Gitea consistency boundary") {
		t.Fatalf("error = %v, want downgraded Gitea consistency refusal", err)
	}
	manifest.Consistency = "service-specific-consistency-boundaries"
	if err := WriteManifest(backupDir, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(backupDir); err == nil || !strings.Contains(err.Error(), "exactly one Gitea boundary") {
		t.Fatalf("error = %v, want missing Gitea boundary refusal", err)
	}
	manifest.ConsistencyBoundaries = []ConsistencyBoundary{{
		Service: "gitea", Method: "unproven-copy",
		StartedAt: createdAt.Add(-time.Minute), CompletedAt: createdAt,
	}}
	if err := WriteManifest(backupDir, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(backupDir); err == nil || !strings.Contains(err.Error(), "unsupported consistency boundary") {
		t.Fatalf("error = %v, want unsupported boundary refusal", err)
	}
}

func TestVerifyKeepsLegacyV2GiteaRecoveryPointsReadable(t *testing.T) {
	root := t.TempDir()
	backupID := "20260808-100000"
	backupDir := filepath.Join(root, backupID)
	if err := os.MkdirAll(filepath.Join(backupDir, "stack-definitions", "gitea"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "stack-definitions", "gitea", "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := BuildChecksummedManifestFiles(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Version: LegacyManifestVersion, ID: backupID,
		CreatedAt:    time.Date(2026, 8, 8, 10, 1, 0, 0, time.UTC),
		ActiveStacks: []string{"gitea"}, StackDefinitions: true,
		Consistency: "logical-online", Complete: true, Files: files,
	}
	if err := WriteManifest(backupDir, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(backupDir); err != nil {
		t.Fatalf("legacy v2 Gitea recovery point is no longer readable: %v", err)
	}
}

func installGiteaBoundaryDocker(t *testing.T, root, logPath, statePath string) {
	t.Helper()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$GITEA_BOUNDARY_LOG"
if [[ "${1:-} ${2:-}" == "ps --format" ]]; then
  printf 'gitea-db\ngitea\n'
  exit 0
fi
if [[ "${1:-}" == "stop" ]]; then
  [[ "${FAIL_GITEA_STOP:-false}" != "true" ]]
  exit
fi
if [[ "${1:-} ${2:-} ${3:-}" == "exec gitea-db pg_dump" ]]; then
  printf 'database-before\n'
  exit 0
fi
if [[ "${1:-} ${2:-}" == "start gitea" ]]; then
  if [[ "${FAIL_GITEA_START:-false}" == "true" ]]; then
    exit 1
  fi
  printf 'after\n' > "$GITEA_BOUNDARY_STATE"
  exit 0
fi
if [[ "${1:-}" == "inspect" && "${*: -1}" == "gitea" ]]; then
  printf 'true healthy\n'
  exit 0
fi
printf 'unexpected docker command: %s\n' "$*" >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GITEA_BOUNDARY_LOG", logPath)
	t.Setenv("GITEA_BOUNDARY_STATE", statePath)
	t.Setenv("FAIL_GITEA_STOP", "false")
	t.Setenv("FAIL_GITEA_START", "false")
}

func TestRotateLocalKeepsNewest(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"20260623-120000", "20260624-120000", "20260625-120000"} {
		if err := os.Mkdir(filepath.Join(root, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := rotateLocal(root, 2); err != nil {
		t.Fatal(err)
	}
	if dirExists(filepath.Join(root, "20260623-120000")) {
		t.Fatal("oldest backup still exists")
	}
	if !dirExists(filepath.Join(root, "20260625-120000")) {
		t.Fatal("newest backup was removed")
	}
}

func TestRunRequiresRemoteDeliveryPolicyForEveryBackupKind(t *testing.T) {
	for _, includeImages := range []bool{false, true} {
		t.Run(strconv.FormatBool(includeImages), func(t *testing.T) {
			root := t.TempDir()
			modeFile := filepath.Join(root, "mode")
			if err := os.WriteFile(modeFile, []byte("normal\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			backupEnv := filepath.Join(root, "backup.env")
			if err := os.WriteFile(backupEnv, []byte("BACKUP_REQUIRE_REMOTE_REPOSITORY=false\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Run(context.Background(), config.Config{
				ModeFile:                   modeFile,
				OperationLock:              filepath.Join(root, "operation.lock"),
				BackupOperationLockTimeout: time.Second,
				BackupEnvFile:              backupEnv,
			}, RunOptions{IncludeImages: includeImages})
			if err == nil || !strings.Contains(err.Error(), "BACKUP_REQUIRE_REMOTE_REPOSITORY=true") {
				t.Fatalf("Run() error = %v", err)
			}
		})
	}
}

func TestRotateLocalTypeKeepsStandardAndOfflineRetentionSeparate(t *testing.T) {
	root := t.TempDir()
	for index, id := range []string{"20260621-120000", "20260622-120000", "20260623-120000", "20260624-120000"} {
		dir := filepath.Join(root, id)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if index%2 == 0 {
			writeDeliveredOfflinePoint(t, dir, id)
		}
	}
	if err := rotateLocalType(root, 1, true); err != nil {
		t.Fatal(err)
	}
	if dirExists(filepath.Join(root, "20260621-120000")) || !dirExists(filepath.Join(root, "20260623-120000")) {
		t.Fatal("offline retention did not keep only the newest offline point")
	}
	if !dirExists(filepath.Join(root, "20260622-120000")) || !dirExists(filepath.Join(root, "20260624-120000")) {
		t.Fatal("offline retention removed a standard backup")
	}
}

func TestRotateLocalTypeDoesNotCountInvalidOfflinePoints(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"20260621-120000", "20260622-120000", "20260623-120000"} {
		dir := filepath.Join(root, id)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if id == "20260622-120000" {
			if err := os.WriteFile(filepath.Join(dir, "offline-images.tar"), []byte("invalid"), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeDeliveredOfflinePoint(t, dir, id)
	}
	if err := rotateLocalType(root, 2, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"20260621-120000", "20260623-120000"} {
		if !dirExists(filepath.Join(root, id)) {
			t.Fatalf("valid offline point %s was removed", id)
		}
	}
}

func writeDeliveredOfflinePoint(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "offline-images.tar"), []byte("images"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repository.bundle"), []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "stack-definitions"), 0o700); err != nil {
		t.Fatal(err)
	}
	files, err := BuildManifestFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	createdAt, err := time.Parse("20060102-150405", id)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Version:              ManifestVersion,
		ID:                   id,
		CreatedAt:            createdAt,
		CLIRevision:          "0123456789abcdef",
		OfflineImages:        true,
		OfflineImageArchives: []OfflineImageArchive{{Source: "example/app:1", ArchiveTag: "admin-node-backup.local/" + id + ":image-001", ImageID: "sha256:test"}},
		StackDefinitions:     true,
		RepositoryBundle:     true,
		Artifacts:            []ManifestArtifact{{Path: "remote-delivery", Required: true, Status: ArtifactProduced, External: true}},
		Complete:             true,
		Files:                files,
	}
	if err := WriteManifest(dir, manifest); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryContentsPathPreservesDotSuffix(t *testing.T) {
	got := directoryContentsPath(filepath.Join("tmp", "snapshot"))
	want := filepath.Join("tmp", "snapshot") + string(os.PathSeparator) + "."
	if got != want {
		t.Fatalf("directoryContentsPath() = %q, want %q", got, want)
	}
}

func assertBackupMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func TestWriteManifestFromRunOptionsTime(t *testing.T) {
	dir := t.TempDir()
	createdAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	if err := WriteManifest(dir, Manifest{Version: 1, ID: "id", CreatedAt: createdAt}); err != nil {
		t.Fatal(err)
	}
	manifest, ok, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !manifest.CreatedAt.Equal(createdAt) {
		t.Fatalf("manifest = %#v ok=%t", manifest, ok)
	}
}

func TestVerifyV4UsesMetadataWithoutContentChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := BuildManifestFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(dir, Manifest{Version: ManifestVersion, ID: "20260625-120000", Complete: true, Files: files}); err != nil {
		t.Fatal(err)
	}
	if files[0].SHA256 != "" {
		t.Fatalf("v4 manifest unexpectedly contains SHA-256: %#v", files[0])
	}
	if err := os.WriteFile(path, []byte("AFTER!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir); err != nil {
		t.Fatalf("same-size content change should be delegated to Restic: %v", err)
	}
}

func TestVerifyV3StillRejectsTamperedBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := BuildChecksummedManifestFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(dir, Manifest{Version: ChecksummedManifestVersion, ID: "20260625-120000", Complete: true, Files: files}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("AFTER!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v, want historical checksum failure", err)
	}
}

func TestOfflineValidationRejectsIncompleteRecoveryManifest(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Manifest)
	}{
		{name: "missing repository bundle declaration", mutate: func(manifest *Manifest) { manifest.RepositoryBundle = false }},
		{name: "missing stack definitions declaration", mutate: func(manifest *Manifest) { manifest.StackDefinitions = false }},
		{name: "missing image mappings", mutate: func(manifest *Manifest) { manifest.OfflineImageArchives = nil }},
		{name: "incomplete image mapping", mutate: func(manifest *Manifest) { manifest.OfflineImageArchives[0].ImageID = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "offline-images.tar"), []byte("images"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "repository.bundle"), []byte("bundle"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(dir, "stack-definitions"), 0o700); err != nil {
				t.Fatal(err)
			}
			files, err := BuildManifestFiles(dir)
			if err != nil {
				t.Fatal(err)
			}
			manifest := Manifest{
				Version:              ManifestVersion,
				ID:                   "20260809-120000",
				CreatedAt:            time.Now().UTC(),
				CLIRevision:          "0123456789abcdef",
				OfflineImages:        true,
				OfflineImageArchives: []OfflineImageArchive{{Source: "example/app:1", ArchiveTag: "admin-node-backup.local/test:image-001", ImageID: "sha256:test"}},
				StackDefinitions:     true,
				RepositoryBundle:     true,
				Complete:             true,
				Files:                files,
			}
			test.mutate(&manifest)
			if err := WriteManifest(dir, manifest); err != nil {
				t.Fatal(err)
			}
			verified, err := Verify(dir)
			if err == nil {
				err = validateScheduledOfflineRecoveryManifest(verified, dir)
			}
			if err == nil {
				t.Fatal("offline validation accepted an incomplete recovery manifest")
			}
		})
	}
}
