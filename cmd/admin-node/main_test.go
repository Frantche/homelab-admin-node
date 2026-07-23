package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

func TestSubcommandsExist(t *testing.T) {
	tests := [][]string{
		{"backup", "list"},
		{"validate", "harbor"},
		{"validate", "openbao"},
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
