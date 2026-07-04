package openbao

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGetStatusAcceptsOpenBaoSealedExitCode(t *testing.T) {
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
if [[ "${1:-}" == "exec" && "$*" == *"bao status -format=json"* ]]; then
  cat <<'JSON'
{"initialized":false,"sealed":true}
JSON
  exit 2
fi
echo unexpected docker "$@" >&2
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	st, err := getStatus(context.Background(), "openbao")
	if err != nil {
		t.Fatal(err)
	}
	if st.Initialized || !st.Sealed {
		t.Fatalf("status = %#v", st)
	}
}

func TestInitIfNeededInitializesWhenSecretsExistButOpenBaoIsUninitialized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake commands are unix-specific")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeDocker := filepath.Join(binDir, "docker")
	if err := os.WriteFile(fakeDocker, []byte(`#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "exec" && "$*" == *"bao status -format=json"* ]]; then
  cat <<'JSON'
{"initialized":false,"sealed":true}
JSON
  exit 2
fi
if [[ "${1:-}" == "exec" && "$*" == *"bao operator init"* ]]; then
  cat <<'JSON'
{"unseal_keys_b64":["key1","key2","key3","key4","key5"],"root_token":"root-token"}
JSON
  exit 0
fi
echo unexpected docker "$@" >&2
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeAgeKeygen := filepath.Join(binDir, "age-keygen")
	if err := os.WriteFile(fakeAgeKeygen, []byte("#!/usr/bin/env bash\necho age1publickey\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeSops := filepath.Join(binDir, "sops")
	if err := os.WriteFile(fakeSops, []byte("#!/usr/bin/env bash\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ageKey := filepath.Join(root, "age.key")
	if err := os.WriteFile(ageKey, []byte("AGE-SECRET-KEY-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secretsDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secretsFile := filepath.Join(secretsDir, "openbao-unseal.sops.yaml")
	if err := os.WriteFile(secretsFile, []byte("old-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := InitIfNeeded(context.Background(), Options{
		AgeKey:      ageKey,
		SecretsDir:  secretsDir,
		SecretsFile: secretsFile,
		KeysetName:  "test-keyset",
		Container:   "openbao",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(secretsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `root_token: "root-token"`) {
		t.Fatalf("secrets file was not replaced with init output: %s", content)
	}
	backups, err := filepath.Glob(secretsFile + ".preinit-backup-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want one backup", backups)
	}
	backupContent, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(backupContent) != "old-secret\n" {
		t.Fatalf("backup content = %q", backupContent)
	}
}
