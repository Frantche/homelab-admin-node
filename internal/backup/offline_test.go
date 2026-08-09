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
	if err := os.WriteFile(filepath.Join(backupDir, "repository.bundle"), []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(backupDir, "stack-definitions"), 0o700); err != nil {
		t.Fatal(err)
	}
	files, err := BuildManifestFiles(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(backupDir, Manifest{
		Version:              ManifestVersion,
		ID:                   "20260808-110000",
		CreatedAt:            now.Add(-time.Hour),
		CLIRevision:          "0123456789abcdef",
		OfflineImages:        true,
		OfflineImageArchives: []OfflineImageArchive{{Source: "example/app:1", ArchiveTag: "admin-node-backup.local/test:image-001", ImageID: "sha256:test"}},
		StackDefinitions:     true,
		RepositoryBundle:     true,
		Artifacts:            []ManifestArtifact{{Path: "remote-delivery", Required: true, Status: ArtifactProduced, External: true}},
		Complete:             true,
		Files:                files,
	}); err != nil {
		t.Fatal(err)
	}
	prereq := filepath.Join(root, "prereq")
	for path, content := range map[string]string{
		filepath.Join(prereq, "age"):               "AGE-SECRET-KEY-TEST",
		filepath.Join(prereq, "config/.git"):       "gitdir: present",
		filepath.Join(prereq, "openbao.sops.yaml"): "value: ENC[test]\nsops:\n  version: 3.13.2",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inventoryPath := filepath.Join(root, "inventory.json")
	inventory, _ := json.Marshal(recoveryKitInventory{LastVerifiedAt: now.Add(-24 * time.Hour), AgeIdentityOffsite: true, ConfigRepoAccessTested: true, ResticAccessOffsite: true, OpenBaoRecoveryOffsite: true, SeparationOfDutiesVerified: true})
	if err := os.WriteFile(inventoryPath, inventory, 0o600); err != nil {
		t.Fatal(err)
	}
	backupEnv := filepath.Join(root, "backup.env")
	if err := os.WriteFile(backupEnv, []byte("RESTIC_REPOSITORIES=offsite\nRESTIC_REPOSITORY_OFFSITE=s3:https://example.invalid/bucket\nRESTIC_PASSWORD_OFFSITE=present\n"), 0o600); err != nil {
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

func TestDeliveredOfflineRecoveryRequiresRemoteEvidence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "repository.bundle"), []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "stack-definitions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateDeliveredOfflineRecoveryManifest(Manifest{OfflineImages: true, RepositoryBundle: true, CLIRevision: "revision", StackDefinitions: true}, dir); err == nil {
		t.Fatal("delivery validation accepted a manifest without remote evidence")
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

func TestResticRecoveryKitDeclarationRequiresMatchingNonLocalPair(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name    string
		content string
		want    bool
	}{
		{name: "matching remote repository", content: "RESTIC_REPOSITORIES=offsite\nRESTIC_REPOSITORY_OFFSITE=s3:https://example.invalid/bucket\nRESTIC_PASSWORD_OFFSITE=secret\n", want: true},
		{name: "invalid remote repository", content: "RESTIC_REPOSITORIES=offsite\nRESTIC_REPOSITORY_OFFSITE=s3:test\nRESTIC_PASSWORD_OFFSITE=secret\n"},
		{name: "matching local repository", content: "RESTIC_REPOSITORIES=offsite\nRESTIC_REPOSITORY_OFFSITE=/srv/backups\nRESTIC_PASSWORD_OFFSITE=secret\n"},
		{name: "matching relative repository", content: "RESTIC_REPOSITORIES=offsite\nRESTIC_REPOSITORY_OFFSITE=backups/restic\nRESTIC_PASSWORD_OFFSITE=secret\n"},
		{name: "stale legacy remote pair", content: "RESTIC_REPOSITORIES=local\nRESTIC_REPOSITORY_LOCAL=/srv/backups\nRESTIC_PASSWORD_LOCAL=secret\nRESTIC_REPOSITORY=s3:stale\nRESTIC_PASSWORD=stale-secret\n"},
		{name: "different identifiers", content: "RESTIC_REPOSITORIES=one two\nRESTIC_REPOSITORY_ONE=s3:https://example.invalid/bucket\nRESTIC_PASSWORD_TWO=secret\n"},
		{name: "empty quoted password", content: "RESTIC_REPOSITORIES=offsite\nRESTIC_REPOSITORY_OFFSITE=s3:https://example.invalid/bucket\nRESTIC_PASSWORD_OFFSITE=\"\"\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name+".env")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := resticOffsiteAccessDeclared(path); got != test.want {
				t.Fatalf("resticOffsiteAccessDeclared() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestCheckOfflineStatusRejectsUnknownRecoveryKitFields(t *testing.T) {
	root := t.TempDir()
	inventoryPath := filepath.Join(root, "inventory.json")
	if err := os.WriteFile(inventoryPath, []byte(`{"last_verified_at":"2026-08-09T12:00:00Z","unexpected_secret":"must-not-be-accepted"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := CheckOfflineStatus(config.Config{BackupRoot: filepath.Join(root, "backups"), RecoveryKitInventoryFile: inventoryPath}, time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if status.RecoveryKitComplete || len(status.Problems) < 2 {
		t.Fatalf("status = %#v", status)
	}
}

func TestRecoveryMaterialMarkersCannotBeComments(t *testing.T) {
	root := t.TempDir()
	ageFile := filepath.Join(root, "age.txt")
	if err := os.WriteFile(ageFile, []byte("# AGE-SECRET-KEY-FAKE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sopsFile := filepath.Join(root, "openbao.sops.yaml")
	if err := os.WriteFile(sopsFile, []byte("# value: ENC[fake]\n# sops:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ageIdentityPresent(ageFile) || sopsEncryptedMaterialPresent(sopsFile) {
		t.Fatal("comment-only recovery material was accepted")
	}
}
