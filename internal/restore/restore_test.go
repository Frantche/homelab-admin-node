package restore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

func TestResolveRejectsPathTraversal(t *testing.T) {
	if _, _, err := Resolve(t.TempDir(), "../outside"); err == nil {
		t.Fatal("expected invalid backup id")
	}
}

func TestSnapshotArtifactSourceSupportsNormalizedAndLegacyLayouts(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".gitea-backup")
	for _, name := range []string{"gitea", "postgres"} {
		if err := os.MkdirAll(filepath.Join(legacy, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got, err := snapshotArtifactSource(root, ".gitea-", "gitea", "postgres")
	if err != nil || got != legacy {
		t.Fatalf("legacy source = %q, %v", got, err)
	}

	for _, name := range []string{"gitea", "postgres"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got, err = snapshotArtifactSource(root, ".gitea-", "gitea", "postgres")
	if err != nil || got != root {
		t.Fatalf("normalized source = %q, %v", got, err)
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
	sealBackupV2(t, backupDir, "20260625-120000")
	err := Run(context.Background(), config.Config{
		AdminRoot:  filepath.Join(root, "admin"),
		ModeFile:   restoreModeFile(t, root),
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
	restoreMarker := filepath.Join(root, "openbao-snapshot-restored")
	chownMarker := filepath.Join(root, "openbao-snapshot-chowned")
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
  if [[ "$*" == *"-format=json"* ]]; then
    echo '{"initialized": true, "sealed": false}'
    exit 0
  fi
  echo "Initialized true"
  echo "Sealed false"
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"chown openbao:openbao /tmp/openbao.snap"* ]]; then
  touch "` + chownMarker + `"
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"snapshot restore"* ]]; then
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
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
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
	if _, err := os.Stat(chownMarker); err != nil {
		t.Fatal("openbao snapshot ownership was not fixed")
	}
	if _, err := os.Stat(restoreMarker); err != nil {
		t.Fatal("openbao snapshot restore was not called")
	}
}

func TestRestoreOpenBaoReadsTokenFromSecretFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
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
  if [[ "$*" == *"-format=json"* ]]; then
    echo '{"initialized": true, "sealed": false}'
    exit 0
  fi
  echo "Initialized true"
  echo "Sealed false"
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"chown openbao:openbao /tmp/openbao.snap"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "${VAULT_TOKEN:-}" == "file-token" && "$*" == *"snapshot restore"* ]]; then
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
	t.Setenv("OPENBAO_TOKEN", "")

	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "secrets/openbao-root-token"), []byte("file-token\n"), 0o600); err != nil {
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
	if _, err := os.Stat(restoreMarker); err != nil {
		t.Fatal("openbao snapshot restore did not use token file")
	}
}

func TestRestoreOpenBaoBootstrapsEmptyRaftBeforeSnapshotRestore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initMarker := filepath.Join(root, "openbao-initialized")
	restoreMarker := filepath.Join(root, "openbao-snapshot-restored")
	fakeDocker := filepath.Join(binDir, "docker")
	fakeDockerScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "compose" || "${1:-}" == "cp" ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"bao status -format=json"* ]]; then
  if [[ -f "` + initMarker + `" ]]; then
    echo '{"initialized":true,"sealed":false}'
  else
    echo '{"initialized":false,"sealed":true}'
  fi
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"operator init -key-shares=1"* ]]; then
  touch "` + initMarker + `"
  echo '{"unseal_keys_b64":["temporary-key"],"root_token":"temporary-token"}'
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"operator unseal temporary-key"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"chown openbao:openbao /tmp/openbao.snap"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "${VAULT_TOKEN:-}" == "temporary-token" && "$*" == *"snapshot restore"* ]]; then
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
	t.Setenv("OPENBAO_TOKEN", "")

	snapPath := filepath.Join(root, "openbao.snap")
	if err := os.WriteFile(snapPath, []byte("snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreOpenBao(context.Background(), config.Config{AdminRoot: root, RepoRoot: filepath.Join(root, "repo")}, filepath.Join(root, "compose.yaml"), snapPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(restoreMarker); err != nil {
		t.Fatal("snapshot restore did not use the temporary root token")
	}
}

func TestFixOpenBaoDataPermissionsSetsRootMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data/openbao")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fixOpenBaoDataPermissions(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("mode=%#o, want 0750", info.Mode().Perm())
	}
}

func TestOpenBaoTokenReadsSOPSSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake sops script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeSOPS := filepath.Join(binDir, "sops")
	fakeSOPSScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *"--decrypt --output-type json"* ]]; then
  printf '{"openbao":{"root_token":"sops-token"},"openbao_config":{"root_token":"config-token"}}'
  exit 0
fi
echo unexpected sops "$@" >&2
exit 1
`
	if err := os.WriteFile(fakeSOPS, []byte(fakeSOPSScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENBAO_TOKEN", "")

	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "secrets/openbao-unseal.sops.yaml"), []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}

	token, err := openBaoToken(context.Background(), config.Config{RepoRoot: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	if token != "sops-token" {
		t.Fatalf("token=%q, want sops-token", token)
	}
}

func TestRunRestoresHarborDumpWithPgRestore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "docker.log")
	fakeDocker := filepath.Join(binDir, "docker")
	fakeDockerScript := `#!/usr/bin/env bash
set -euo pipefail
printf '%q ' "$@" >> "` + logPath + `"
printf '\n' >> "` + logPath + `"
if [[ "${1:-}" == "compose" ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"pg_isready -U postgres"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"psql -U postgres -d postgres"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"pg_restore --exit-on-error --no-owner --no-privileges -U postgres -d registry"* ]]; then
  cat >/dev/null
  exit 0
fi
echo unexpected docker "$@" >&2
exit 1
`
	if err := os.WriteFile(fakeDocker, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	backupRoot := filepath.Join(root, "backups")
	backupDir := filepath.Join(backupRoot, "20260625-120000")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "harbor.dump"), []byte("harbor-dump"), 0o644); err != nil {
		t.Fatal(err)
	}
	sealBackupV2(t, backupDir, "20260625-120000")
	adminRoot := filepath.Join(root, "admin")
	if err := os.MkdirAll(filepath.Join(adminRoot, "stacks/harbor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(adminRoot, "env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "stacks/harbor/compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "env/harbor.env"), []byte("HARBOR_DB_PASSWORD=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Run(context.Background(), config.Config{
		AdminRoot:  adminRoot,
		ModeFile:   restoreModeFile(t, root),
		BackupRoot: backupRoot,
	}, Options{
		ID:       "20260625-120000",
		Validate: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(log)
	if !strings.Contains(calls, "up -d harbor-db") {
		t.Fatalf("harbor db was not started: %s", calls)
	}
	if !strings.Contains(calls, "DROP\\ DATABASE\\ IF\\ EXISTS\\ \\\"registry\\\"\\;") {
		t.Fatalf("registry database was not dropped: %s", calls)
	}
	if !strings.Contains(calls, "CREATE\\ DATABASE\\ \\\"registry\\\"\\ OWNER\\ \\\"postgres\\\"\\;") {
		t.Fatalf("registry database was not created: %s", calls)
	}
	if !strings.Contains(calls, "pg_restore --exit-on-error --no-owner --no-privileges -U postgres -d registry") {
		t.Fatalf("pg_restore was not called: %s", calls)
	}
}

func TestRunPgRestoreFailureSetsRestoreFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeDocker := filepath.Join(binDir, "docker")
	fakeDockerScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "compose" ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"pg_isready -U keycloak"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"psql -U keycloak -d postgres"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"pg_restore --exit-on-error --no-owner --no-privileges -U keycloak -d keycloak"* ]]; then
  echo "restore failed" >&2
  exit 1
fi
echo unexpected docker "$@" >&2
exit 1
`
	if err := os.WriteFile(fakeDocker, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	backupRoot := filepath.Join(root, "backups")
	backupDir := filepath.Join(backupRoot, "20260625-120000")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "keycloak.dump"), []byte("keycloak-dump"), 0o644); err != nil {
		t.Fatal(err)
	}
	sealBackupV2(t, backupDir, "20260625-120000")
	adminRoot := filepath.Join(root, "admin")
	if err := os.MkdirAll(filepath.Join(adminRoot, "stacks/keycloak"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(adminRoot, "env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "stacks/keycloak/compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "env/keycloak.env"), []byte("KEYCLOAK_DB_PASSWORD=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	modeFile := filepath.Join(root, "mode")
	restoreModeFileAt(t, modeFile)

	err := Run(context.Background(), config.Config{
		AdminRoot:  adminRoot,
		ModeFile:   modeFile,
		BackupRoot: backupRoot,
	}, Options{ID: "20260625-120000"})
	if err == nil {
		t.Fatal("expected restore error")
	}
	mode, readErr := os.ReadFile(modeFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(mode) != "restore_failed\n" {
		t.Fatalf("mode = %q", mode)
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

func TestStartStacksSkipsCloudflaredComposeWhenCloudflareDisabled(t *testing.T) {
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

	err := startStacks(context.Background(), config.Config{CloudflareDisabled: true}, stacks{
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
	sealBackupV2(t, backupDir, "20260625-120000")
	adminRoot := filepath.Join(root, "admin")
	if err := os.MkdirAll(filepath.Join(adminRoot, "data/gitea"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "data/gitea/app.ini"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "data/gitea/stale.txt"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeStat, err := os.Stat(filepath.Join(adminRoot, "data/gitea"))
	if err != nil {
		t.Fatal(err)
	}
	modeFile := filepath.Join(root, "mode")
	restoreModeFileAt(t, modeFile)
	validateCalled := false

	err = Run(context.Background(), config.Config{
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
	if _, err := os.Stat(filepath.Join(adminRoot, "data/gitea/stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file should have been removed, err = %v", err)
	}
	stat, err := os.Stat(filepath.Join(adminRoot, "data/gitea"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(beforeStat, stat) {
		t.Fatal("gitea data directory should be preserved during restore")
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

func sealBackupV2(t *testing.T, dir, id string) {
	t.Helper()
	files, err := backup.BuildManifestFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = backup.WriteManifest(dir, backup.Manifest{Version: backup.ManifestVersion, ID: id, CreatedAt: time.Now().UTC(), Complete: true, Consistency: "test", Files: files})
	if err != nil {
		t.Fatal(err)
	}
}

func restoreModeFile(t *testing.T, root string) string {
	t.Helper()
	return restoreModeFileAt(t, filepath.Join(root, "mode"))
}

func restoreModeFileAt(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("restore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunMissingBackupSetsRestoreFailed(t *testing.T) {
	root := t.TempDir()
	modeFile := filepath.Join(root, "mode")
	restoreModeFileAt(t, modeFile)
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
