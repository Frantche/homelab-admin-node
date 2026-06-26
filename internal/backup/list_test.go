package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListBackupsWithoutManifest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "20260625-120000")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keycloak.sql"), []byte("sql"), 0o644); err != nil {
		t.Fatal(err)
	}

	backups, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("len(backups) = %d, want 1", len(backups))
	}
	if backups[0].ID != "20260625-120000" {
		t.Fatalf("ID = %q", backups[0].ID)
	}
	if !backups[0].HasKeycloakDump {
		t.Fatal("HasKeycloakDump = false, want true")
	}
}

func TestListBackupsSortsNewestFirst(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"20260624-120000", "20260625-120000"} {
		if err := os.Mkdir(filepath.Join(root, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	backups, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if backups[0].ID != "20260625-120000" {
		t.Fatalf("newest ID = %q", backups[0].ID)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	createdAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	manifest := Manifest{Version: 1, ID: "backup-id", CreatedAt: createdAt, Hostname: "host", OfflineImages: true, Images: []string{"busybox:latest"}}
	if err := WriteManifest(dir, manifest); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("manifest not found")
	}
	if got.ID != manifest.ID || !got.OfflineImages || len(got.Images) != 1 {
		t.Fatalf("manifest = %#v", got)
	}
}
