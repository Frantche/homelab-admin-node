package mode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRejectsUnknownMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mode")
	if err := os.WriteFile(path, []byte("typo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestSetWritesValidModeAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "mode")
	if err := Set(path, "normal"); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "normal" {
		t.Fatalf("mode = %q", got)
	}
	if err := Set(path, "unknown"); err == nil {
		t.Fatal("expected invalid mode error")
	}
}
