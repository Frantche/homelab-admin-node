package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("ADMIN_NODE_REPO_ROOT", "")
	t.Setenv("ADMIN_NODE_ROOT", "")
	t.Setenv("CI_MODE", "")
	t.Setenv("RESTIC_BACKUP_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))

	cfg := FromEnv()

	if cfg.RepoRoot != DefaultRepoRoot {
		t.Fatalf("RepoRoot = %q, want %q", cfg.RepoRoot, DefaultRepoRoot)
	}
	if cfg.AdminRoot != DefaultAdminRoot {
		t.Fatalf("AdminRoot = %q, want %q", cfg.AdminRoot, DefaultAdminRoot)
	}
	if cfg.AdminNodeLANIP != DefaultAdminNodeLANIP {
		t.Fatalf("AdminNodeLANIP = %q, want %q", cfg.AdminNodeLANIP, DefaultAdminNodeLANIP)
	}
	if cfg.BackupOperationLockTimeout != 30*time.Minute {
		t.Fatalf("BackupOperationLockTimeout = %s, want 30m", cfg.BackupOperationLockTimeout)
	}
	if cfg.GiteaBackupQuiesceTimeout != 10*time.Minute {
		t.Fatalf("GiteaBackupQuiesceTimeout = %s, want 10m", cfg.GiteaBackupQuiesceTimeout)
	}
	if cfg.CIMode {
		t.Fatal("CIMode = true, want false")
	}
	if cfg.PiholeDisabled {
		t.Fatal("PiholeDisabled = true, want false")
	}
	if cfg.CloudflareDisabled {
		t.Fatal("CloudflareDisabled = true, want false")
	}
	if !cfg.ObservabilityDisabled {
		t.Fatal("ObservabilityDisabled = false, want true")
	}
}

func TestFromEnvReadsStackFlagsFromBackupEnv(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	if err := os.WriteFile(envFile, []byte("CLOUDFLARE_ENABLED=\"false\"\nOBSERVABILITY_ENABLED=\"true\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RESTIC_BACKUP_ENV_FILE", envFile)
	t.Setenv("CLOUDFLARE_ENABLED", "")
	t.Setenv("OBSERVABILITY_ENABLED", "")
	os.Unsetenv("CLOUDFLARE_ENABLED")
	os.Unsetenv("OBSERVABILITY_ENABLED")

	cfg := FromEnv()
	if !cfg.CloudflareDisabled {
		t.Fatal("CloudflareDisabled = false, want true")
	}
	if cfg.ObservabilityDisabled {
		t.Fatal("ObservabilityDisabled = true, want false")
	}

	t.Setenv("OBSERVABILITY_ENABLED", "false")
	if cfg := FromEnv(); !cfg.ObservabilityDisabled {
		t.Fatal("explicit OBSERVABILITY_ENABLED did not override backup.env")
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("ADMIN_NODE_REPO_ROOT", "/tmp/repo")
	t.Setenv("ADMIN_NODE_ROOT", "/tmp/admin")
	t.Setenv("KEYCLOAK_DOMAIN", "keycloak.test")
	t.Setenv("ADMIN_NODE_LAN_IP", "10.0.0.10")
	t.Setenv("CI_MODE", "true")
	t.Setenv("CI_MOCK_PIHOLE", "true")
	t.Setenv("PIHOLE_ENABLED", "false")
	t.Setenv("CLOUDFLARE_ENABLED", "false")
	t.Setenv("OBSERVABILITY_ENABLED", "true")
	t.Setenv("ADMIN_NODE_VALIDATE_MOCK_ALL", "true")
	t.Setenv("BACKUP_OPERATION_LOCK_TIMEOUT", "45m")
	t.Setenv("BACKUP_GITEA_QUIESCE_TIMEOUT", "8m")

	cfg := FromEnv()

	if cfg.RepoRoot != "/tmp/repo" {
		t.Fatalf("RepoRoot = %q", cfg.RepoRoot)
	}
	if cfg.AdminRoot != "/tmp/admin" {
		t.Fatalf("AdminRoot = %q", cfg.AdminRoot)
	}
	if cfg.BackupStatusRoot != "/tmp/admin/backups/status" {
		t.Fatalf("BackupStatusRoot = %q", cfg.BackupStatusRoot)
	}
	if cfg.KeycloakDomain != "keycloak.test" {
		t.Fatalf("KeycloakDomain = %q", cfg.KeycloakDomain)
	}
	if cfg.AdminNodeLANIP != "10.0.0.10" {
		t.Fatalf("AdminNodeLANIP = %q", cfg.AdminNodeLANIP)
	}
	if !cfg.CIMode {
		t.Fatal("CIMode = false, want true")
	}
	if !cfg.CIMockPihole {
		t.Fatal("CIMockPihole = false, want true")
	}
	if !cfg.PiholeDisabled {
		t.Fatal("PiholeDisabled = false, want true")
	}
	if !cfg.CloudflareDisabled {
		t.Fatal("CloudflareDisabled = false, want true")
	}
	if cfg.ObservabilityDisabled {
		t.Fatal("ObservabilityDisabled = true, want false")
	}
	if !cfg.ValidateMockAll {
		t.Fatal("ValidateMockAll = false, want true")
	}
	if cfg.BackupOperationLockTimeout != 45*time.Minute {
		t.Fatalf("BackupOperationLockTimeout = %s, want 45m", cfg.BackupOperationLockTimeout)
	}
	if cfg.GiteaBackupQuiesceTimeout != 8*time.Minute {
		t.Fatalf("GiteaBackupQuiesceTimeout = %s, want 8m", cfg.GiteaBackupQuiesceTimeout)
	}
}
