package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

func TestCheckOfflineStatusVerifiesRecoveryPointAndRecoveryKit(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	backupDir := filepath.Join(root, "backups", "20260808-110000")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "offline-images.tar"), []byte("images"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := BuildManifestFiles(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(backupDir, Manifest{Version: ManifestVersion, ID: "20260808-110000", CreatedAt: now.Add(-time.Hour), OfflineImages: true, Complete: true, Files: files}); err != nil {
		t.Fatal(err)
	}
	prereq := filepath.Join(root, "prereq")
	for _, path := range []string{filepath.Join(prereq, "age"), filepath.Join(prereq, "config/.git"), filepath.Join(prereq, "openbao.sops.yaml")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("present"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inventoryPath := filepath.Join(root, "inventory.json")
	inventory, _ := json.Marshal(recoveryKitInventory{LastVerifiedAt: now.Add(-24 * time.Hour), AgeIdentityOffsite: true, ConfigRepoAccessTested: true, ResticAccessOffsite: true, OpenBaoRecoveryOffsite: true, SeparationOfDutiesVerified: true})
	if err := os.WriteFile(inventoryPath, inventory, 0o600); err != nil {
		t.Fatal(err)
	}
	backupEnv := filepath.Join(root, "backup.env")
	if err := os.WriteFile(backupEnv, []byte("RESTIC_REPOSITORY_OFFSITE=s3:test\nRESTIC_PASSWORD_OFFSITE=present\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := CheckOfflineStatus(config.Config{
		BackupRoot:               filepath.Join(root, "backups"),
		OfflineBackupMaxAge:      8 * 24 * time.Hour,
		RecoveryKitInventoryFile: inventoryPath,
		RecoveryKitMaxAge:        90 * 24 * time.Hour,
		AgeKeyFile:               filepath.Join(prereq, "age"),
		ConfigRepoRoot:           filepath.Join(prereq, "config"),
		OpenBaoRecoveryFile:      filepath.Join(prereq, "openbao.sops.yaml"),
		BackupEnvFile:            backupEnv,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if status.ID != "20260808-110000" || !status.Fresh || !status.Verified || !status.RecoveryKitComplete || len(status.Problems) != 0 {
		t.Fatalf("status = %#v", status)
	}
}

func TestCheckOfflineStatusReportsMissingPrerequisites(t *testing.T) {
	status, err := CheckOfflineStatus(config.Config{BackupRoot: t.TempDir(), OfflineBackupMaxAge: time.Hour, RecoveryKitInventoryFile: filepath.Join(t.TempDir(), "missing")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Problems) < 2 || status.Fresh || status.Verified || status.RecoveryKitComplete {
		t.Fatalf("status = %#v", status)
	}
}
