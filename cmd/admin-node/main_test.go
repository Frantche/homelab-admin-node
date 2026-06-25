package main

import (
	"bytes"
	"context"
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

func TestSubcommandsExist(t *testing.T) {
	tests := [][]string{
		{"backup", "list"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			cfg := config.FromEnv()
			cfg.BackupRoot = t.TempDir()
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
