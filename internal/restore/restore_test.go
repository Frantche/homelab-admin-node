package restore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Frantche/homelab-admin-node/internal/backup"
	"github.com/Frantche/homelab-admin-node/internal/config"
)

func TestResolveLatest(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"20260624-120000", "20260625-120000"} {
		if err := os.Mkdir(filepath.Join(root, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	info, ok, err := Resolve(root, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || info.ID != "20260625-120000" {
		t.Fatalf("info=%#v ok=%t", info, ok)
	}
}

func TestRunLoadsOfflineImages(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	loadMarker := filepath.Join(root, "docker-load-called")
	fakeDocker := filepath.Join(binDir, "docker")
	if err := os.WriteFile(fakeDocker, []byte("#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"load\" ]]; then touch \""+loadMarker+"\"; exit 0; fi\necho unexpected >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	backupRoot := filepath.Join(root, "backups")
	backupDir := filepath.Join(backupRoot, "20260625-120000")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "offline-images.tar"), []byte("images"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), config.Config{
		AdminRoot:  filepath.Join(root, "admin"),
		ModeFile:   filepath.Join(root, "mode"),
		BackupRoot: backupRoot,
	}, Options{
		ID:       "20260625-120000",
		Validate: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(loadMarker); err != nil {
		t.Fatal("docker load was not called")
	}
}

func TestRestoreOpenBaoUnsealsBeforeSnapshotRestore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unsealMarker := filepath.Join(root, "openbao-unsealed")
	restoreMarker := filepath.Join(root, "openbao-snapshot-restored")
	fakeDocker := filepath.Join(binDir, "docker")
	fakeDockerScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "compose" ]]; then
  exit 0
fi
if [[ "${1:-}" == "cp" ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"bao status"* ]]; then
  echo "Initialized true"
  echo "Sealed true"
  exit 2
fi
if [[ "${1:-}" == "exec" && "$*" == *"snapshot restore"* ]]; then
  if [[ ! -f "` + unsealMarker + `" ]]; then
    echo "Vault is sealed" >&2
    exit 2
  fi
  touch "` + restoreMarker + `"
  exit 0
fi
echo unexpected docker "$@" >&2
exit 1
`
	if err := os.WriteFile(fakeDocker, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENBAO_TOKEN", "token")

	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	unsealScript := filepath.Join(repoRoot, "scripts/openbao-unseal.sh")
	if err := os.WriteFile(unsealScript, []byte("#!/usr/bin/env bash\nset -euo pipefail\ntouch \""+unsealMarker+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	snapPath := filepath.Join(root, "openbao.snap")
	if err := os.WriteFile(snapPath, []byte("snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := restoreOpenBao(context.Background(), config.Config{RepoRoot: repoRoot}, filepath.Join(root, "compose.yaml"), snapPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unsealMarker); err != nil {
		t.Fatal("openbao unseal script was not called")
	}
	if _, err := os.Stat(restoreMarker); err != nil {
		t.Fatal("openbao snapshot restore was not called")
	}
}

func TestStartStacksSkipsCloudflaredComposeInCIMockMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeDocker := filepath.Join(binDir, "docker")
	if err := os.WriteFile(fakeDocker, []byte("#!/usr/bin/env bash\necho unexpected docker call >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cloudflaredCompose := filepath.Join(root, "cloudflared.yaml")
	if err := os.WriteFile(cloudflaredCompose, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := startStacks(context.Background(), config.Config{CIMockCloudflareTunnel: true}, stacks{
		CloudflaredCompose: cloudflaredCompose,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSelectBackupByNumber(t *testing.T) {
	var out bytes.Buffer
	id, err := Select(bytes.NewBufferString("2\n"), &out, []backup.Info{{ID: "a"}, {ID: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if id != "b" {
		t.Fatalf("id = %q", id)
	}
}

func TestRunRestoresGiteaDataAndSetsNormalMode(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	backupDir := filepath.Join(backupRoot, "20260625-120000")
	if err := os.MkdirAll(filepath.Join(backupDir, "gitea-data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "gitea-data/app.ini"), []byte("restored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adminRoot := filepath.Join(root, "admin")
	if err := os.MkdirAll(filepath.Join(adminRoot, "data/gitea"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "data/gitea/app.ini"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	modeFile := filepath.Join(root, "mode")
	validateCalled := false

	err := Run(context.Background(), config.Config{
		AdminRoot:  adminRoot,
		ModeFile:   modeFile,
		BackupRoot: backupRoot,
	}, Options{
		ID: "20260625-120000",
		Validate: func(context.Context) error {
			validateCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !validateCalled {
		t.Fatal("validation callback was not called")
	}
	content, err := os.ReadFile(filepath.Join(adminRoot, "data/gitea/app.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "restored\n" {
		t.Fatalf("gitea data = %q", content)
	}
	stat, err := os.Stat(filepath.Join(adminRoot, "data/gitea"))
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm() != 0o755 {
		t.Fatalf("gitea data mode = %o", stat.Mode().Perm())
	}
	mode, err := os.ReadFile(modeFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(mode) != "normal\n" {
		t.Fatalf("mode = %q", mode)
	}
}

func TestRunMissingBackupSetsRestoreFailed(t *testing.T) {
	root := t.TempDir()
	modeFile := filepath.Join(root, "mode")
	err := Run(context.Background(), config.Config{
		AdminRoot:  filepath.Join(root, "admin"),
		ModeFile:   modeFile,
		BackupRoot: filepath.Join(root, "backups"),
	}, Options{ID: "missing"})
	if err == nil {
		t.Fatal("expected missing backup error")
	}
	mode, readErr := os.ReadFile(modeFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(mode) != "restore_failed\n" {
		t.Fatalf("mode = %q", mode)
	}
}
