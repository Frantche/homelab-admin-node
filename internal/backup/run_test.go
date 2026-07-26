package backup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
  exit 0
fi
if [[ "${1:-}" == "cp" ]]; then
  echo "openbao-snap" > "$3"
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
	for _, dir := range []string{"stacks", "env", "data/gitea"} {
		if err := os.MkdirAll(filepath.Join(adminRoot, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
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
exit 0
`), 0o755); err != nil {
		t.Fatal(err)
	}
	backupEnv := filepath.Join(root, "backup.env")
	if err := os.WriteFile(backupEnv, []byte(`RESTIC_REPOSITORY="/tmp/restic-repo"
RESTIC_PASSWORD="secret"
RESTIC_INIT_REPOSITORIES="true"
RESTIC_DEFAULT_FORGET_ARGS="--keep-last 2 --prune"
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
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	info, err := Run(context.Background(), config.Config{
		AdminRoot:     adminRoot,
		ModeFile:      modeFile,
		BackupRoot:    filepath.Join(root, "backups"),
		BackupEnvFile: filepath.Join(root, "missing-backup.env"),
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

func TestDirectoryContentsPathPreservesDotSuffix(t *testing.T) {
	got := directoryContentsPath(filepath.Join("tmp", "snapshot"))
	want := filepath.Join("tmp", "snapshot") + string(os.PathSeparator) + "."
	if got != want {
		t.Fatalf("directoryContentsPath() = %q, want %q", got, want)
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

func TestVerifyRejectsTamperedBackup(t *testing.T) {
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
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir); err == nil {
		t.Fatal("expected checksum failure")
	}
}
