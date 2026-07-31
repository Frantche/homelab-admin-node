package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallAgeKeyCopiesContentWithRestrictedPermissions(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source-key.txt")
	dst := filepath.Join(root, "nested", "keys.txt")
	key := "AGE-SECRET-KEY-TEST-ONLY\n"
	if err := os.WriteFile(src, []byte(key), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallAgeKey(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != key {
		t.Fatal("installed key differs from source")
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Fatalf("mode = %o, want 0400", info.Mode().Perm())
	}
}

func TestInstallAgeKeyRejectsInvalidSource(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "keys.txt")

	err := InstallAgeKey(filepath.Join(root, "missing"), dst)
	if err == nil {
		t.Fatal("missing source unexpectedly accepted")
	}

	err = InstallAgeKey(root, dst)
	if err == nil {
		t.Fatal("directory source unexpectedly accepted")
	}
}
