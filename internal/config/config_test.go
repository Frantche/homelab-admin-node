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
	if cfg.OfflineBackupRetention != 2 || cfg.OfflineBackupMaxAge != 8*24*time.Hour || cfg.RecoveryKitMaxAge != 90*24*time.Hour {
		t.Fatalf("offline defaults = retention %d max age %s kit age %s", cfg.OfflineBackupRetention, cfg.OfflineBackupMaxAge, cfg.RecoveryKitMaxAge)
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
	t.Setenv("BACKUP_OFFLINE_RETENTION", "4")
	t.Setenv("BACKUP_OFFLINE_MAX_AGE", "10h")
	t.Setenv("BACKUP_OFFLINE_MIN_FREE_BYTES", "1234")

	cfg := FromEnv()

	if cfg.RepoRoot != "/tmp/repo" {
		t.Fatalf("RepoRoot = %q", cfg.RepoRoot)
	}
	if cfg.AdminRoot != "/tmp/admin" {
		t.Fatalf("AdminRoot = %q", cfg.AdminRoot)
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
	if cfg.OfflineBackupRetention != 4 || cfg.OfflineBackupMaxAge != 10*time.Hour || cfg.OfflineBackupMinFreeBytes != 1234 {
		t.Fatalf("offline overrides = %#v", cfg)
	}
}
