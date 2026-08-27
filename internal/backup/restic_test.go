package backup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadResticConfigMultiRepo(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	if err := os.WriteFile(envFile, []byte(`RESTIC_REPOSITORIES="local sftp"
RESTIC_INIT_REPOSITORIES="true"
RESTIC_REPOSITORY_LOCAL="/srv/restic"
RESTIC_PASSWORD_LOCAL="local-pass"
RESTIC_FORGET_ARGS_LOCAL="none"
RESTIC_REPOSITORY_SFTP="sftp:backup:/srv/restic"
RESTIC_PASSWORD_SFTP="sftp-pass"
AWS_ACCESS_KEY_ID_SFTP="ignored-but-parsed"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories) != 2 || cfg.Repositories[0] != "local" || !cfg.InitRepositories {
		t.Fatalf("config = %#v", cfg)
	}
	if got := cfg.RepoValues["LOCAL"]["RESTIC_PASSWORD"]; got != "local-pass" {
		t.Fatalf("LOCAL password = %q", got)
	}
	if got := cfg.RepoValues["SFTP"]["AWS_ACCESS_KEY_ID"]; got != "ignored-but-parsed" {
		t.Fatalf("SFTP env = %q", got)
	}
}

func TestLoadResticConfigSingleQuotedValues(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	if err := os.WriteFile(envFile, []byte(`RESTIC_DEFAULT_FORGET_ARGS='--keep-last 3 --prune'
RESTIC_REPOSITORIES=local
RESTIC_REPOSITORY_LOCAL=/srv/admin/backups/restic
RESTIC_PASSWORD_LOCAL='ci-restic-pass'
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultForgetArgs != "--keep-last 3 --prune" {
		t.Fatalf("forget args = %q", cfg.DefaultForgetArgs)
	}
	if got := cfg.RepoValues["LOCAL"]["RESTIC_PASSWORD"]; got != "ci-restic-pass" {
		t.Fatalf("password = %q", got)
	}
}

func TestValidateSecureRepository(t *testing.T) {
	if err := validateSecureRepository("ftp://example/restic", true); err == nil {
		t.Fatal("expected insecure ftp repository to fail")
	}
	if err := validateSecureRepository("sftp:backup:/srv/restic", true); err != nil {
		t.Fatal(err)
	}
	if err := validateSecureRepository("ftp://example/restic", false); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureResticCacheEnvWithoutUserHome(t *testing.T) {
	cacheHome := filepath.Join(t.TempDir(), "restic-cache")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("ADMIN_NODE_RESTIC_CACHE_HOME", cacheHome)

	if err := ensureResticCacheEnv(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("XDG_CACHE_HOME"); got != cacheHome {
		t.Fatalf("XDG_CACHE_HOME = %q, want %q", got, cacheHome)
	}
	info, err := os.Stat(cacheHome)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", cacheHome)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("cache mode = %o, want 0700", got)
	}
}

func TestRunResticFailsWhenRequiredBinaryIsMissing(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "backup.env")
	content := `BACKUP_REQUIRE_REMOTE_REPOSITORY=true
RESTIC_REPOSITORIES=offsite
RESTIC_REPOSITORY_OFFSITE=sftp:backup:/srv/restic
RESTIC_PASSWORD_OFFSITE=test-password
`
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)

	_, err := RunRestic(context.Background(), envFile, []string{filepath.Join(root, "backup")})
	if err == nil || !strings.Contains(err.Error(), "restic is required") {
		t.Fatalf("error = %v, want required restic binary failure", err)
	}
}

func TestRunResticAllowsMissingBinaryInExplicitLocalOnlyMode(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "backup.env")
	if err := os.WriteFile(envFile, []byte("BACKUP_REQUIRE_REMOTE_REPOSITORY=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)

	if _, err := RunRestic(context.Background(), envFile, []string{filepath.Join(root, "backup")}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadResticConfigRejectsRequiredLocalOnlyRepository(t *testing.T) {
	for _, repository := range []string{"/srv/admin/backups/restic", "backups/restic", "./backups/restic", "file:backups/restic"} {
		t.Run(repository, func(t *testing.T) {
			envFile := filepath.Join(t.TempDir(), "backup.env")
			content := `BACKUP_REQUIRE_REMOTE_REPOSITORY=true
RESTIC_REPOSITORIES=local
RESTIC_REPOSITORY_LOCAL=` + repository + `
RESTIC_PASSWORD_LOCAL=test-password
`
			if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadResticConfig(envFile); err == nil || !strings.Contains(err.Error(), "non-local") {
				t.Fatalf("error = %v, want required remote repository failure", err)
			}
		})
	}
}

func TestLoadResticConfigAllowsLocalRepositoryOnlyInCIMode(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	content := "CI_MODE=true\n" +
		"BACKUP_REQUIRE_REMOTE_REPOSITORY=true\n" +
		"RESTIC_REPOSITORIES=local\n" +
		"RESTIC_REPOSITORY_LOCAL=/tmp/ci-restic\n" +
		"RESTIC_PASSWORD_LOCAL=test-password\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CIMode || !hasRemoteRepository(cfg) {
		t.Fatalf("CI repository was not accepted as remote mock: %#v", cfg)
	}
}

func TestVerifyResticSnapshotFailsWhenDeliveryCannotBeObserved(t *testing.T) {
	root := t.TempDir()
	resticPath := filepath.Join(root, "restic")
	if err := os.WriteFile(resticPath, []byte("#!/bin/bash\nset -euo pipefail\nprintf '[]\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	if err := verifyResticSnapshot(context.Background(), os.Environ(), nil, "admin-node-run:test"); err == nil || !strings.Contains(err.Error(), "found 0") {
		t.Fatalf("error = %v, want missing snapshot failure", err)
	}
}

func TestRunResticUsesCompatibleParentAndRestoresRelativeLayout(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	backupRoot := filepath.Join(root, "backups")
	envFile := filepath.Join(root, "backup.env")
	content := "RESTIC_REPOSITORIES=local\n" +
		"RESTIC_INIT_REPOSITORIES=true\n" +
		"RESTIC_REPOSITORY_LOCAL=" + repository + "\n" +
		"RESTIC_PASSWORD_LOCAL=test-password\n" +
		"RESTIC_FORGET_ARGS_LOCAL=none\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	firstID := "20260826-030000"
	secondID := "20260827-030000"
	first := writeResticTestBackup(t, backupRoot, firstID, ManifestVersion)
	if _, err := RunRestic(context.Background(), envFile, []string{first}); err != nil {
		t.Fatal(err)
	}
	second := writeResticTestBackup(t, backupRoot, secondID, ManifestVersion)
	if _, err := RunRestic(context.Background(), envFile, []string{second}); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), "RESTIC_REPOSITORY="+repository, "RESTIC_PASSWORD=test-password")
	cmd := exec.Command("restic", "snapshots", "--json")
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []struct {
		ID     string   `json:"id"`
		Parent string   `json:"parent"`
		Tags   []string `json:"tags"`
	}
	if err := json.Unmarshal(output, &snapshots); err != nil {
		t.Fatal(err)
	}
	byBackupID := map[string]struct {
		ID     string
		Parent string
	}{}
	for _, snapshot := range snapshots {
		for _, tag := range snapshot.Tags {
			if strings.HasPrefix(tag, "backup-id:") {
				byBackupID[strings.TrimPrefix(tag, "backup-id:")] = struct {
					ID     string
					Parent string
				}{snapshot.ID, snapshot.Parent}
			}
		}
	}
	if byBackupID[secondID].Parent != byBackupID[firstID].ID || byBackupID[firstID].ID == "" {
		t.Fatalf("second snapshot parent = %q, want %q", byBackupID[secondID].Parent, byBackupID[firstID].ID)
	}

	ls := exec.Command("restic", "ls", byBackupID[secondID].ID)
	ls.Env = env
	listing, err := ls.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(listing), "/payload") || strings.Contains(string(listing), "/"+secondID+"/") {
		t.Fatalf("unexpected relative snapshot layout:\n%s", listing)
	}

	if err := os.RemoveAll(second); err != nil {
		t.Fatal(err)
	}
	if err := RestoreFromRestic(context.Background(), envFile, "local", backupRoot, secondID); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(second, "payload")); err != nil || string(data) != "unchanged payload\n" {
		t.Fatalf("restored payload = %q, error = %v", data, err)
	}

	legacyID := "20260824-030000"
	legacy := writeResticTestBackup(t, backupRoot, legacyID, ChecksummedManifestVersion)
	legacyBackup := exec.Command("restic", "backup", "--tag", "admin-node-v2", "--tag", "backup-id:"+legacyID, legacy)
	legacyBackup.Env = env
	if output, err := legacyBackup.CombinedOutput(); err != nil {
		t.Fatalf("create legacy snapshot: %v: %s", err, output)
	}
	thirdID := "20260828-030000"
	third := writeResticTestBackup(t, backupRoot, thirdID, ManifestVersion)
	retainedContent := strings.Replace(content, "RESTIC_FORGET_ARGS_LOCAL=none", "RESTIC_FORGET_ARGS_LOCAL=--keep-last 1", 1)
	if err := os.WriteFile(envFile, []byte(retainedContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunRestic(context.Background(), envFile, []string{third}); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("restic", "snapshots", "--json")
	cmd.Env = env
	output, err = cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	snapshots = nil
	if err := json.Unmarshal(output, &snapshots); err != nil {
		t.Fatal(err)
	}
	legacyFound := false
	layoutBackups := map[string]bool{}
	for _, snapshot := range snapshots {
		hasRelativeLayout := false
		backupID := ""
		for _, tag := range snapshot.Tags {
			hasRelativeLayout = hasRelativeLayout || tag == resticLayoutTag
			if strings.HasPrefix(tag, "backup-id:") {
				backupID = strings.TrimPrefix(tag, "backup-id:")
			}
		}
		if backupID == legacyID {
			legacyFound = true
		}
		if hasRelativeLayout {
			layoutBackups[backupID] = true
		}
	}
	if !legacyFound {
		t.Fatal("new-layout retention deleted a historical absolute-path snapshot")
	}
	if len(layoutBackups) != 1 || !layoutBackups[thirdID] {
		t.Fatalf("relative-layout retention kept unexpected backups: %#v", layoutBackups)
	}
}

func TestRestoreFromResticKeepsAbsoluteV3LayoutCompatible(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	backupRoot := filepath.Join(root, "backups")
	id := "20260825-030000"
	dir := writeResticTestBackup(t, backupRoot, id, ChecksummedManifestVersion)
	envFile := filepath.Join(root, "backup.env")
	content := "RESTIC_REPOSITORIES=local\n" +
		"RESTIC_REPOSITORY_LOCAL=" + repository + "\n" +
		"RESTIC_PASSWORD_LOCAL=test-password\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "RESTIC_REPOSITORY="+repository, "RESTIC_PASSWORD=test-password")
	for _, args := range [][]string{{"init"}, {"backup", "--tag", "backup-id:" + id, dir}} {
		cmd := exec.Command("restic", args...)
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("restic %v: %v: %s", args, err, output)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := RestoreFromRestic(context.Background(), envFile, "local", backupRoot, id); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir); err != nil {
		t.Fatalf("restored v3 backup is invalid: %v", err)
	}
}

func TestCheckResticRotatesSubsetOnlyAfterEveryRepositorySucceeds(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "restic.log")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s|%s\\n' \"$RESTIC_REPOSITORY\" \"$*\" >> \"" + logPath + "\"\nif [[ \"${FAIL_SECOND_REPOSITORY:-false}\" == true && \"$RESTIC_REPOSITORY\" == *repo-two ]]; then exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(binDir, "restic"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	envFile := filepath.Join(root, "backup.env")
	content := "RESTIC_REPOSITORIES='one two'\n" +
		"RESTIC_REPOSITORY_ONE=" + filepath.Join(root, "repo-one") + "\n" +
		"RESTIC_PASSWORD_ONE=password\n" +
		"RESTIC_REPOSITORY_TWO=" + filepath.Join(root, "repo-two") + "\n" +
		"RESTIC_PASSWORD_TWO=password\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	statusRoot := filepath.Join(root, "status")
	t.Setenv("FAIL_SECOND_REPOSITORY", "true")
	if _, err := CheckRestic(context.Background(), envFile, statusRoot); err == nil {
		t.Fatal("expected second repository failure")
	}
	if subset, err := loadIntegritySubset(statusRoot); err != nil || subset != 1 {
		t.Fatalf("subset after failure = %d, error = %v", subset, err)
	}
	t.Setenv("FAIL_SECOND_REPOSITORY", "false")
	if _, err := CheckRestic(context.Background(), envFile, statusRoot); err != nil {
		t.Fatal(err)
	}
	if subset, err := loadIntegritySubset(statusRoot); err != nil || subset != 2 {
		t.Fatalf("subset after success = %d, error = %v", subset, err)
	}
	if _, err := CheckRestic(context.Background(), envFile, statusRoot); err != nil {
		t.Fatal(err)
	}
	if subset, err := loadIntegritySubset(statusRoot); err != nil || subset != 3 {
		t.Fatalf("subset after second success = %d, error = %v", subset, err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(logData), "1/4") != 4 || strings.Count(string(logData), "2/4") != 2 {
		t.Fatalf("unexpected subset calls:\n%s", logData)
	}
}

func writeResticTestBackup(t *testing.T, backupRoot, id string, version int) string {
	t.Helper()
	dir := filepath.Join(backupRoot, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payload"), []byte("unchanged payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := BuildManifestFiles(dir)
	if version < ManifestVersion {
		files, err = BuildChecksummedManifestFiles(dir)
	}
	if err != nil {
		t.Fatal(err)
	}
	createdAt, err := time.Parse("20060102-150405", id)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(dir, Manifest{Version: version, ID: id, CreatedAt: createdAt, Consistency: "test", Complete: true, Files: files}); err != nil {
		t.Fatal(err)
	}
	return dir
}
