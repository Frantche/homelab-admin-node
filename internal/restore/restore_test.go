package restore

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

func TestRestoreRepositoryRevisionUsesBundledBackupCommit(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		output, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q")
	git("config", "user.name", "Restore Test")
	git("config", "user.email", "restore-test@example.invalid")
	tracked := filepath.Join(repoRoot, "revision")
	if err := os.WriteFile(tracked, []byte("backup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "revision")
	git("commit", "-qm", "backup revision")
	backupRevision := git("rev-parse", "HEAD")

	backupPath := filepath.Join(root, "backup")
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	git("bundle", "create", filepath.Join(backupPath, "repository.bundle"), "HEAD")

	if err := os.WriteFile(tracked, []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "newer main revision")
	if git("rev-parse", "HEAD") == backupRevision {
		t.Fatal("test repository did not advance")
	}

	if err := restoreRepositoryRevision(context.Background(), repoRoot, backupPath, backupRevision); err != nil {
		t.Fatal(err)
	}
	if got := git("rev-parse", "HEAD"); got != backupRevision {
		t.Fatalf("restored revision = %s, want %s", got, backupRevision)
	}
	content, err := os.ReadFile(tracked)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "backup\n" {
		t.Fatalf("restored repository content = %q", content)
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
	oldStackMarker := filepath.Join(root, "old-stack-started")
	fakeDocker := filepath.Join(binDir, "docker")
	fakeDockerScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "load" ]]; then
  touch "` + loadMarker + `"
  exit 0
fi
if [[ "${1:-} ${2:-} ${3:-}" == "image inspect --format" ]]; then
  echo "sha256:offline-image-id"
  exit 0
fi
if [[ "${1:-}" == "compose" ]]; then
  if [[ "$*" == *"up --pull never -d"* ]]; then
    compose_file=""
    for ((i=1; i <= $#; i++)); do
      if [[ "${!i}" == "-f" ]]; then
        next=$((i + 1))
        compose_file="${!next}"
      fi
    done
    grep -q "example.invalid/app:backup" "$compose_file"
    touch "` + oldStackMarker + `"
  fi
  exit 0
fi
echo unexpected docker "$@" >&2
exit 1
`
	if err := os.WriteFile(fakeDocker, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	adminRoot := filepath.Join(root, "admin")
	if err := os.MkdirAll(filepath.Join(adminRoot, "stacks/gitea"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "stacks/gitea/compose.yaml"), []byte("services:\n  app:\n    image: example.invalid/app:main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupRoot := filepath.Join(root, "backups")
	backupDir := filepath.Join(backupRoot, "20260625-120000")
	if err := os.MkdirAll(filepath.Join(backupDir, "stack-definitions/gitea"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "offline-images.tar"), []byte("images"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "stack-definitions/gitea/compose.yaml"), []byte("services:\n  app:\n    image: example.invalid/app:backup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := backup.BuildManifestFiles(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.WriteManifest(backupDir, backup.Manifest{
		Version:       backup.ManifestVersion,
		ID:            "20260625-120000",
		CreatedAt:     time.Now().UTC(),
		CLIRevision:   strings.Repeat("a", 40),
		OfflineImages: true,
		OfflineImageArchives: []backup.OfflineImageArchive{{
			Source:     "example.invalid/app:backup@sha256:source",
			ArchiveTag: "example.invalid/app:backup",
			ImageID:    "sha256:offline-image-id",
		}},
		StackDefinitions: true,
		Consistency:      "test",
		Complete:         true,
		Files:            files,
	}); err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), config.Config{
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
	if _, err := os.Stat(loadMarker); err != nil {
		t.Fatal("docker load was not called")
	}
	if _, err := os.Stat(oldStackMarker); err != nil {
		t.Fatal("restore did not start the stack definition captured by the backup")
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

func TestRestoreHarborDataPreservesRegistryPasswordFile(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backup")
	sourceCore := filepath.Join(backupRoot, "harbor-data/core")
	if err := os.MkdirAll(sourceCore, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePassword := filepath.Join(sourceCore, "registry.passwd")
	if err := os.WriteFile(sourcePassword, []byte("harbor_registry_user:$2y$05$backup\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sourcePassword, 0o640); err != nil {
		t.Fatal(err)
	}

	adminRoot := filepath.Join(root, "admin")
	targetCore := filepath.Join(adminRoot, "data/harbor/core")
	if err := os.MkdirAll(targetCore, 0o755); err != nil {
		t.Fatal(err)
	}
	targetPassword := filepath.Join(targetCore, "registry.passwd")
	if err := os.WriteFile(targetPassword, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := restoreHarborData(backupRoot, adminRoot); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(targetPassword)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "harbor_registry_user:$2y$05$backup\n" {
		t.Fatalf("registry password content = %q", content)
	}
	info, err := os.Stat(targetPassword)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("registry password mode = %#o, want 0640", info.Mode().Perm())
	}
}

func TestRestoreHarborDataRejectsInvalidRegistryPasswordFile(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
		wantErr string
	}{
		{
			name:    "missing",
			prepare: func(*testing.T, string) {},
			wantErr: "is missing",
		},
		{
			name: "empty",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, nil, 0o640); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "is empty",
		},
		{
			name: "not regular",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o750); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "is not a regular file",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			backupRoot := filepath.Join(root, "backup")
			sourceCore := filepath.Join(backupRoot, "harbor-data/core")
			if err := os.MkdirAll(sourceCore, 0o700); err != nil {
				t.Fatal(err)
			}
			test.prepare(t, filepath.Join(sourceCore, "registry.passwd"))

			err := restoreHarborData(backupRoot, filepath.Join(root, "admin"))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("restoreHarborData error = %v, want %q", err, test.wantErr)
			}
		})
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
if [[ "${1:-}" == "exec" && "$*" == *"psql -U postgres -d registry -v ON_ERROR_STOP=1 -c"* && "$*" == *"UPDATE harbor_user"* ]]; then
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
	if !strings.Contains(calls, "UPDATE harbor_user") {
		t.Fatalf("Harbor administrator was not prepared for recovery-kit initialization: %s", calls)
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

func TestRestoreKeycloakAdminKeepsMatchingAdministrator(t *testing.T) {
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
if [[ "$*" == *"openid-configuration"* ]]; then
  exit 0
fi
if [[ "$*" == *"kcadm.sh get serverinfo"* && "${KC_CLI_PASSWORD:-}" == "current-secret" ]]; then
  exit 0
fi
echo unexpected docker "$@" >&2
exit 1
`
	if err := os.WriteFile(fakeDocker, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	adminRoot := filepath.Join(root, "admin")
	if err := os.MkdirAll(filepath.Join(adminRoot, "env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "env/keycloak.env"), []byte("KEYCLOAK_ADMIN=admin\nKEYCLOAK_ADMIN_PASSWORD=current-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RestoreKeycloakAdmin(context.Background(), config.Config{AdminRoot: adminRoot}); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "bootstrap-admin") {
		t.Fatalf("matching administrator should not be recovered: %s", log)
	}
	if strings.Contains(string(log), "current-secret") {
		t.Fatal("administrator password leaked into docker arguments")
	}
}

func TestRestoreKeycloakAdminRecoversMismatchedAdministrator(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "docker.log")
	reconciledMarker := filepath.Join(root, "keycloak-admin-reconciled")
	fakeDocker := filepath.Join(binDir, "docker")
	fakeDockerScript := `#!/usr/bin/env bash
set -euo pipefail
printf '%q ' "$@" >> "` + logPath + `"
printf '\n' >> "` + logPath + `"
if [[ "$*" == *"openid-configuration"* ]]; then
  exit 0
fi
if [[ "$*" == *"bootstrap-admin user"* && "${KEYCLOAK_RECOVERY_ADMIN:-}" == admin-recovery-* && -n "${KEYCLOAK_RECOVERY_PASSWORD:-}" ]]; then
  exit 0
fi
if [[ "$*" == *"kcadm.sh get serverinfo"* && "${KC_CLI_PASSWORD:-}" != "rotated-secret" ]]; then
  exit 0
fi
if [[ "$*" == *"set-password"* && "${KEYCLOAK_TARGET_ADMIN:-}" == "admin" && "${KEYCLOAK_TARGET_PASSWORD:-}" == "rotated-secret" ]]; then
  touch "` + reconciledMarker + `"
  exit 0
fi
if [[ "$*" == *"kcadm.sh get serverinfo"* && "${KC_CLI_PASSWORD:-}" == "rotated-secret" && -f "` + reconciledMarker + `" ]]; then
  exit 0
fi
echo authentication failed >&2
exit 1
`
	if err := os.WriteFile(fakeDocker, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	adminRoot := filepath.Join(root, "admin")
	if err := os.MkdirAll(filepath.Join(adminRoot, "env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "env/keycloak.env"), []byte("KEYCLOAK_ADMIN=admin\nKEYCLOAK_ADMIN_PASSWORD=rotated-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RestoreKeycloakAdmin(context.Background(), config.Config{AdminRoot: adminRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reconciledMarker); err != nil {
		t.Fatal("Keycloak administrator password was not reconciled")
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(log)
	if !strings.Contains(calls, "bootstrap-admin user") {
		t.Fatalf("temporary recovery administrator was not bootstrapped: %s", calls)
	}
	if !strings.Contains(calls, "search=admin-recovery-") {
		t.Fatalf("temporary recovery administrators were not removed: %s", calls)
	}
	if strings.Contains(calls, "rotated-secret") {
		t.Fatal("administrator password leaked into docker arguments")
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

func TestRestoreActiveStackDefinitionsLeavesExtraStackUntouched(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "backup", "stack-definitions")
	adminRoot := filepath.Join(root, "admin")
	for _, path := range []string{
		filepath.Join(sourceRoot, "gitea"),
		filepath.Join(adminRoot, "stacks", "gitea"),
		filepath.Join(adminRoot, "stacks", "extra"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "gitea", "compose.yaml"), []byte("backup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "stacks", "gitea", "compose.yaml"), []byte("current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(adminRoot, "stacks", "extra", "compose.yaml")
	if err := os.WriteFile(extra, []byte("untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := restoreActiveStackDefinitions(sourceRoot, adminRoot, []string{"gitea"}); err != nil {
		t.Fatal(err)
	}
	gitea, err := os.ReadFile(filepath.Join(adminRoot, "stacks", "gitea", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gitea) != "backup\n" {
		t.Fatalf("gitea definition = %q", gitea)
	}
	extraData, err := os.ReadFile(extra)
	if err != nil {
		t.Fatal(err)
	}
	if string(extraData) != "untouched\n" {
		t.Fatalf("extra definition = %q", extraData)
	}
}

func TestFixOpenBaoStackPermissionsMatchesAnsible(t *testing.T) {
	adminRoot := t.TempDir()
	stackRoot := filepath.Join(adminRoot, "stacks", "openbao")
	if err := os.MkdirAll(stackRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"compose.yaml", "openbao.hcl"} {
		if err := os.WriteFile(filepath.Join(stackRoot, name), []byte("test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := fixOpenBaoStackPermissions(adminRoot); err != nil {
		t.Fatal(err)
	}
	assertMode := func(path string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %04o, want %04o", path, got, want)
		}
	}
	assertMode(stackRoot, 0o755)
	assertMode(filepath.Join(stackRoot, "compose.yaml"), 0o644)
	assertMode(filepath.Join(stackRoot, "openbao.hcl"), 0o644)
}

func TestRestoreActiveStackDefinitionsPreservesCapturedModes(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "backup", "stack-definitions")
	adminRoot := filepath.Join(root, "admin")
	fixtures := map[string]os.FileMode{
		"harbor/config/registry.yml":        0o644,
		"observability/otel-collector.yaml": 0o644,
		"openbao/openbao.hcl":               0o644,
	}
	for rel, mode := range fixtures {
		path := filepath.Join(sourceRoot, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
		compose := filepath.Join(sourceRoot, name, "compose.yaml")
		if err := os.WriteFile(compose, []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(compose, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := restoreActiveStackDefinitions(sourceRoot, adminRoot, []string{"harbor", "observability", "openbao"}); err != nil {
		t.Fatal(err)
	}
	for rel, want := range fixtures {
		path := filepath.Join(adminRoot, "stacks", rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %04o, want %04o", path, got, want)
		}
	}
}

func TestStartRestoreStacksUsesOnlyManifestStacks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake docker script is unix-specific")
	}
	root := t.TempDir()
	adminRoot := filepath.Join(root, "admin")
	for _, name := range []string{"extra", "gitea"} {
		dir := filepath.Join(adminRoot, "stacks", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "docker.log")
	script := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> " + strconv.Quote(logPath) + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := startRestoreStacks(context.Background(), config.Config{AdminRoot: adminRoot}, stacks{}, []string{"gitea"}); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "extra") || !strings.Contains(string(log), "stacks/gitea/compose.yaml") {
		t.Fatalf("docker log = %s", log)
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

func TestRunRestoresGiteaSnapshotDataAndLogicalDatabase(t *testing.T) {
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
if [[ "${1:-}" == "exec" && "$*" == *"pg_isready -U gitea"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"psql -U gitea -d postgres"* ]]; then
  exit 0
fi
if [[ "${1:-}" == "exec" && "$*" == *"pg_restore --exit-on-error --no-owner --no-privileges -U gitea -d gitea"* ]]; then
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
	for _, path := range []string{
		filepath.Join(backupDir, "gitea-stack/gitea"),
		filepath.Join(backupDir, "gitea-stack/postgres"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(backupDir, "gitea-stack/gitea/app.ini"), []byte("restored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "gitea-stack/postgres/PG_VERSION"), []byte("old database\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "gitea.dump"), []byte("gitea-dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	sealBackupV2(t, backupDir, "20260625-120000")

	adminRoot := filepath.Join(root, "admin")
	giteaStack := filepath.Join(adminRoot, "data/gitea-stack")
	for _, path := range []string{
		filepath.Join(adminRoot, "stacks/gitea"),
		filepath.Join(adminRoot, "env"),
		filepath.Join(giteaStack, "gitea"),
		filepath.Join(giteaStack, "postgres"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "stacks/gitea/compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "env/gitea.env"), []byte("GITEA_DB_PASSWORD=current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	currentDatabase := filepath.Join(giteaStack, "postgres/PG_VERSION")
	if err := os.WriteFile(currentDatabase, []byte("current database\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Run(context.Background(), config.Config{
		AdminRoot:      adminRoot,
		GiteaStackPath: giteaStack,
		ModeFile:       restoreModeFile(t, root),
		BackupRoot:     backupRoot,
	}, Options{
		ID:       "20260625-120000",
		Validate: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(giteaStack, "gitea/app.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "restored\n" {
		t.Fatalf("gitea data = %q", content)
	}
	content, err = os.ReadFile(currentDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "current database\n" {
		t.Fatalf("physical database was replaced: %q", content)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "pg_restore --exit-on-error --no-owner --no-privileges -U gitea -d gitea") {
		t.Fatalf("logical Gitea dump was not restored: %s", log)
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
