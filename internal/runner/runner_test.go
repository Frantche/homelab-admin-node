package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerCapturesOutputExitCodeEnvironmentAndDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell contract is Unix-specific")
	}
	dir := t.TempDir()
	result := (ExecRunner{
		Dir: dir,
		Env: []string{"ADMIN_NODE_RUNNER_TEST=present"},
	}).Run(context.Background(), "sh", "-c", `printf '%s:%s' "$ADMIN_NODE_RUNNER_TEST" "$PWD"; printf 'failure' >&2; exit 7`)

	if result.Code != 7 {
		t.Fatalf("Code = %d, want 7", result.Code)
	}
	if result.Stdout != "present:"+dir {
		t.Fatalf("Stdout = %q", result.Stdout)
	}
	if result.Stderr != "failure" {
		t.Fatalf("Stderr = %q", result.Stderr)
	}
}

func TestExecRunnerHonorsCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell contract is Unix-specific")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	result := (ExecRunner{}).Run(ctx, "sh", "-c", "sleep 10")

	if result.Code == 0 {
		t.Fatal("cancelled command unexpectedly succeeded")
	}
	if !strings.Contains(result.Stderr, "killed") &&
		!strings.Contains(result.Stderr, "canceled") &&
		!strings.Contains(result.Stderr, "deadline exceeded") {
		t.Fatalf("Stderr = %q, want cancellation error", result.Stderr)
	}
}

func TestExecRunnerReportsMissingExecutable(t *testing.T) {
	name := filepath.Join(t.TempDir(), "missing-command")
	result := (ExecRunner{}).Run(context.Background(), name)
	if result.Code == 0 || result.Stderr == "" {
		t.Fatalf("result = %+v, want a process start failure", result)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("unexpected command path state: %v", err)
	}
}
