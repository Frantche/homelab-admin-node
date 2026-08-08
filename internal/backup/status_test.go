package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteSuccessStatusIsPrivateAndReadable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "status")
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	if err := WriteSuccessStatus(root, StatusStandard, "20260808-100000", now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, StatusStandard+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("marker mode = %o, want 600", got)
	}
	results, err := CheckFreshness(now.Add(time.Hour), filepath.Join(root, "missing.env"), root)
	if err != nil {
		t.Fatal(err)
	}
	standard := freshnessByKind(t, results, StatusStandard)
	if !standard.Fresh || standard.BackupID != "20260808-100000" {
		t.Fatalf("standard status = %#v", standard)
	}
}

func TestCheckFreshnessFailsRequiredMissingAndStaleStatuses(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "backup.env")
	content := `BACKUP_REQUIRE_REMOTE_REPOSITORY=true
GITEA_PROCESS_BACKUP_ENABLED=true
RESTIC_REPOSITORIES=offsite
BACKUP_STANDARD_MAX_AGE=2h
BACKUP_GITEA_PROCESS_MAX_AGE=2h
BACKUP_REMOTE_MAX_AGE=2h
BACKUP_INTEGRITY_MAX_AGE=2h
`
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	if err := WriteSuccessStatus(root, StatusStandard, "20260808-060000", now.Add(-4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	results, err := CheckFreshness(now, envFile, root)
	if err != nil {
		t.Fatal(err)
	}
	if !FreshnessFailed(results) {
		t.Fatal("required stale/missing statuses did not fail freshness")
	}
	if standard := freshnessByKind(t, results, StatusStandard); standard.Fresh || !standard.Present {
		t.Fatalf("standard status = %#v", standard)
	}
	if remote := freshnessByKind(t, results, StatusRemote); !remote.Required || remote.Present {
		t.Fatalf("remote status = %#v", remote)
	}
}

func TestOptionalOfflineStatusDoesNotFailWhenDisabled(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	if err := WriteSuccessStatus(root, StatusStandard, "20260808-100000", now); err != nil {
		t.Fatal(err)
	}
	results, err := CheckFreshness(now, filepath.Join(root, "missing.env"), root)
	if err != nil {
		t.Fatal(err)
	}
	if FreshnessFailed(results) {
		t.Fatalf("optional statuses failed freshness: %#v", results)
	}
	offline := freshnessByKind(t, results, StatusOfflineImages)
	if offline.Required {
		t.Fatalf("offline status unexpectedly required: %#v", offline)
	}
}

func freshnessByKind(t *testing.T, results []FreshnessStatus, kind string) FreshnessStatus {
	t.Helper()
	for _, result := range results {
		if result.Kind == kind {
			return result
		}
	}
	t.Fatalf("status %s not found", kind)
	return FreshnessStatus{}
}
