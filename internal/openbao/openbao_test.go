package openbao

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
