package config

import (
	"os"
	"path/filepath"
	"strings"
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
	t.Setenv("BACKUP_GITEA_QUIESCE_TIMEOUT", "8m")
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
	if cfg.OfflineBackupRetention != 4 || cfg.OfflineBackupMaxAge != 10*time.Hour || cfg.OfflineBackupMinFreeBytes != 1234 {
		t.Fatalf("offline overrides = %#v", cfg)
	}
}

func TestLoadUsesManagedRuntimeFileWithProcessPrecedence(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	content := `# managed values
CI_MODE=true
ADMIN_NODE_REPO_ROOT='/managed/repo'
ADMIN_NODE_ROOT="/managed/admin"
ADMIN_BACKUP_ROOT=/managed/backups
ADMIN_MODE_FILE=/managed/mode
ADMIN_RESTORE_ID_FILE=/managed/restore-id
ADMIN_NODE_LAN_IP=192.0.2.41
ADMIN_OPERATION_LOCK=/managed/operation.lock
ADMIN_SNAPSHOT_ROOT=/managed/snapshots
GITEA_STACK_PATH=/managed/gitea-stack
KEYCLOAK_DOMAIN="keycloak.deployed.test"
HARBOR_DOMAIN=harbor.deployed.test
GITEA_DOMAIN=gitea.deployed.test
TRAEFIK_DOMAIN=traefik.deployed.test
OPENBAO_DOMAIN=bao.deployed.test
PIHOLE_ENABLED=false
CLOUDFLARE_ENABLED=false
OBSERVABILITY_ENABLED=true
BACKUP_REQUIRE_BTRFS_HOT=true
BACKUP_REQUIRE_HARBOR_READ_ONLY=true
BACKUP_LOCAL_RETENTION=7
BACKUP_OPERATION_LOCK_TIMEOUT=45m
BACKUP_GITEA_QUIESCE_TIMEOUT=9m
BACKUP_OFFLINE_RETENTION=5
BACKUP_OFFLINE_MAX_AGE=240h
BACKUP_OFFLINE_MIN_FREE_BYTES=1234
BACKUP_RECOVERY_KIT_INVENTORY=/managed/recovery-kit.json
BACKUP_RECOVERY_KIT_MAX_AGE=720h
SOPS_AGE_KEY_FILE=/managed/age-keys.txt
ADMIN_CONFIG_REPO_ROOT=/managed/config
OPENBAO_RECOVERY_FILE=/managed/openbao-recovery.sops.yaml
HARBOR_ADMIN_PASSWORD="this deliberately unterminated secret is ignored
`
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RESTIC_BACKUP_ENV_FILE", envFile)
	t.Setenv("GITEA_DOMAIN", "gitea.process.test")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ManagedRuntimeFileLoaded {
		t.Fatal("managed runtime file was not marked loaded")
	}
	if !cfg.CIMode {
		t.Fatal("managed CI_MODE was not loaded")
	}
	if cfg.RepoRoot != "/managed/repo" || cfg.AdminRoot != "/managed/admin" || cfg.BackupRoot != "/managed/backups" {
		t.Fatalf("managed paths not loaded: %#v", cfg)
	}
	if cfg.RestoreIDFile != "/managed/restore-id" || cfg.OperationLock != "/managed/operation.lock" || cfg.SnapshotRoot != "/managed/snapshots" || cfg.GiteaStackPath != "/managed/gitea-stack" {
		t.Fatalf("managed operation paths not loaded: %#v", cfg)
	}
	if cfg.KeycloakDomain != "keycloak.deployed.test" || cfg.HarborDomain != "harbor.deployed.test" || cfg.GiteaDomain != "gitea.process.test" || cfg.TraefikDomain != "traefik.deployed.test" || cfg.OpenBaoDomain != "bao.deployed.test" {
		t.Fatalf("domain precedence is incorrect: %#v", cfg)
	}
	if !cfg.PiholeDisabled || !cfg.CloudflareDisabled || cfg.ObservabilityDisabled {
		t.Fatalf("managed feature flags not loaded: %#v", cfg)
	}
	if !cfg.RequireBtrfsHotBackup || !cfg.RequireHarborReadOnly || cfg.LocalBackupRetention != 7 || cfg.BackupOperationLockTimeout != 45*time.Minute {
		t.Fatalf("managed backup policy not loaded: %#v", cfg)
	}
	if cfg.GiteaBackupQuiesceTimeout != 9*time.Minute {
		t.Fatalf("managed Gitea quiesce timeout not loaded: %s", cfg.GiteaBackupQuiesceTimeout)
	}
	if cfg.OfflineBackupRetention != 5 || cfg.OfflineBackupMaxAge != 240*time.Hour || cfg.OfflineBackupMinFreeBytes != 1234 {
		t.Fatalf("managed offline backup policy not loaded: %#v", cfg)
	}
	if cfg.RecoveryKitInventoryFile != "/managed/recovery-kit.json" || cfg.RecoveryKitMaxAge != 720*time.Hour || cfg.AgeKeyFile != "/managed/age-keys.txt" || cfg.ConfigRepoRoot != "/managed/config" || cfg.OpenBaoRecoveryFile != "/managed/openbao-recovery.sops.yaml" {
		t.Fatalf("managed recovery prerequisites not loaded: %#v", cfg)
	}
	if err := cfg.ValidateOperational(); err != nil {
		t.Fatalf("deployed domains rejected: %v", err)
	}
}

func TestLoadMissingManagedRuntimeFileUsesSafeDefaults(t *testing.T) {
	t.Setenv("RESTIC_BACKUP_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	for _, key := range []string{"KEYCLOAK_DOMAIN", "HARBOR_DOMAIN", "GITEA_DOMAIN", "TRAEFIK_DOMAIN", "OPENBAO_DOMAIN"} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ManagedRuntimeFileLoaded {
		t.Fatal("missing managed runtime file was marked loaded")
	}
	if err := cfg.ValidateOperational(); err == nil || !strings.Contains(err.Error(), "built-in example values") {
		t.Fatalf("expected clear incomplete runtime error, got %v", err)
	}
}

func TestLoadRejectsMalformedManagedEntriesWithoutExposingValues(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		marker  string
	}{
		{name: "missing assignment", content: "KEYCLOAK_DOMAIN\n", marker: "expected KEY=VALUE"},
		{name: "malformed quote", content: "KEYCLOAK_DOMAIN=\"private-value\n", marker: "invalid double-quoted value"},
		{name: "invalid boolean", content: "PIHOLE_ENABLED=private-invalid\n", marker: "invalid runtime configuration value for PIHOLE_ENABLED"},
		{name: "invalid CI mode", content: "CI_MODE=private-invalid\n", marker: "invalid runtime configuration value for CI_MODE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			envFile := filepath.Join(t.TempDir(), "backup.env")
			if err := os.WriteFile(envFile, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RESTIC_BACKUP_ENV_FILE", envFile)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("Load() error = %v, want marker %q", err, test.marker)
			}
			if strings.Contains(err.Error(), "private-value") || strings.Contains(err.Error(), "private-invalid") {
				t.Fatalf("runtime error exposed a value: %v", err)
			}
		})
	}
}
