package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	DefaultRepoRoot                = "/opt/homelab-admin-node"
	DefaultAdminRoot               = "/srv/admin"
	DefaultModeFile                = "/etc/admin-node/mode"
	DefaultReleaseRefFile          = "/etc/admin-node/release-ref"
	DefaultReleaseNameFile         = "/etc/admin-node/release-name"
	DefaultReleaseChannelFile      = "/etc/admin-node/release-channel"
	DefaultQualificationFile       = "/etc/admin-node/qualification.json"
	DefaultGitRefFile              = "/etc/admin-node/git-ref"
	DefaultSchemaFile              = "/etc/admin-node/config-schema-version"
	DefaultPackageSnapshotFile     = "/etc/admin-node/package-snapshot"
	DefaultPackageSnapshotModeFile = "/etc/admin-node/package-snapshot-mode"
	DefaultRestoreIDFile           = "/etc/admin-node/restore-id"
	DefaultBackupRoot              = "/srv/admin/backups/local"
	DefaultBackupEnvFile           = "/srv/admin/env/backup.env"
	DefaultOperationLock           = "/run/admin-node-operation.lock"
	DefaultGiteaStackPath          = "/srv/admin/data/gitea-stack"
	DefaultSnapshotRoot            = "/srv/admin/backups/snapshots"
	DefaultBackupStatusRoot        = "/srv/admin/backups/status"
	DefaultAdminNodeLANIP          = "192.168.1.10"
	DefaultKeycloakDomain          = "keycloak.example.com"
	DefaultHarborDomain            = "harbor.example.com"
	DefaultGiteaDomain             = "git.example.com"
	DefaultTraefikDomain           = "traefik.example.com"
	DefaultOpenBaoDomain           = "bao.example.com"
)

type Config struct {
	RepoRoot                   string
	AdminRoot                  string
	ModeFile                   string
	ReleaseRefFile             string
	ReleaseNameFile            string
	ReleaseChannelFile         string
	QualificationFile          string
	GitRefFile                 string
	SchemaVersionFile          string
	PackageSnapshotFile        string
	PackageSnapshotModeFile    string
	RestoreIDFile              string
	BackupRoot                 string
	BackupEnvFile              string
	OperationLock              string
	GiteaStackPath             string
	SnapshotRoot               string
	BackupStatusRoot           string
	RequireBtrfsHotBackup      bool
	RequireHarborReadOnly      bool
	LocalBackupRetention       int
	BackupOperationLockTimeout time.Duration
	GiteaBackupQuiesceTimeout  time.Duration
	OfflineBackupRetention     int
	OfflineBackupMaxAge        time.Duration
	OfflineBackupMinFreeBytes  int64
	RecoveryKitInventoryFile   string
	RecoveryKitMaxAge          time.Duration
	AgeKeyFile                 string
	ConfigRepoRoot             string
	OpenBaoRecoveryFile        string
	AdminNodeLANIP             string
	KeycloakDomain             string
	HarborDomain               string
	GiteaDomain                string
	TraefikDomain              string
	OpenBaoDomain              string
	CIMode                     bool
	CIMockPihole               bool
	CIMockCloudflareTunnel     bool
	PiholeDisabled             bool
	CloudflareDisabled         bool
	ObservabilityDisabled      bool
	SkipPublicURLValidation    bool
	CISkipPublicURLValidation  bool
	ValidateMockAll            bool
	ManagedRuntimeFileLoaded   bool
}

func FromEnv() Config {
	cfg, err := Load()
	if err == nil {
		return cfg
	}
	cfg, _ = load(nil, false)
	return cfg
}

func Load() (Config, error) {
	backupEnvFile := getenv("RESTIC_BACKUP_ENV_FILE", DefaultBackupEnvFile)
	values, loaded, err := readManagedRuntimeFile(backupEnvFile)
	if err != nil {
		return Config{}, err
	}
	return load(values, loaded)
}

func load(values map[string]string, loaded bool) (Config, error) {
	resolve := func(key, fallback string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		if value := values[key]; value != "" {
			return value
		}
		return fallback
	}
	resolveBool := func(key string, fallback bool) (bool, error) {
		value := resolve(key, "")
		if value == "" {
			return fallback, nil
		}
		return strconv.ParseBool(value)
	}
	resolveInt := func(key string, fallback int) (int, error) {
		value := resolve(key, "")
		if value == "" {
			return fallback, nil
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return 0, invalidManagedValue(key)
		}
		return parsed, nil
	}
	resolveInt64 := func(key string, fallback int64) (int64, error) {
		value := resolve(key, "")
		if value == "" {
			return fallback, nil
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return 0, invalidManagedValue(key)
		}
		return parsed, nil
	}
	resolveDuration := func(key string, fallback time.Duration) (time.Duration, error) {
		value := resolve(key, "")
		if value == "" {
			return fallback, nil
		}
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return 0, invalidManagedValue(key)
		}
		return parsed, nil
	}
	resolveNonNegativeDuration := func(key string, fallback time.Duration) (time.Duration, error) {
		value := resolve(key, "")
		if value == "" {
			return fallback, nil
		}
		if value == "0" {
			return 0, nil
		}
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < 0 {
			return 0, invalidManagedValue(key)
		}
		return parsed, nil
	}

	requireBtrfs, err := resolveBool("BACKUP_REQUIRE_BTRFS_HOT", false)
	if err != nil {
		return Config{}, invalidManagedValue("BACKUP_REQUIRE_BTRFS_HOT")
	}
	requireHarborReadOnly, err := resolveBool("BACKUP_REQUIRE_HARBOR_READ_ONLY", false)
	if err != nil {
		return Config{}, invalidManagedValue("BACKUP_REQUIRE_HARBOR_READ_ONLY")
	}
	localRetention, err := resolveInt("BACKUP_LOCAL_RETENTION", 3)
	if err != nil {
		return Config{}, err
	}
	lockTimeout, err := resolveDuration("BACKUP_OPERATION_LOCK_TIMEOUT", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	giteaQuiesceTimeout, err := resolveDuration("BACKUP_GITEA_QUIESCE_TIMEOUT", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	offlineRetention, err := resolveInt("BACKUP_OFFLINE_RETENTION", 2)
	if err != nil {
		return Config{}, err
	}
	offlineMaxAge, err := resolveNonNegativeDuration("BACKUP_OFFLINE_MAX_AGE", 8*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	offlineMinFreeBytes, err := resolveInt64("BACKUP_OFFLINE_MIN_FREE_BYTES", 0)
	if err != nil {
		return Config{}, err
	}
	recoveryKitMaxAge, err := resolveDuration("BACKUP_RECOVERY_KIT_MAX_AGE", 90*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	piholeEnabled, err := resolveBool("PIHOLE_ENABLED", true)
	if err != nil {
		return Config{}, invalidManagedValue("PIHOLE_ENABLED")
	}
	cloudflareEnabled, err := resolveBool("CLOUDFLARE_ENABLED", true)
	if err != nil {
		return Config{}, invalidManagedValue("CLOUDFLARE_ENABLED")
	}
	observabilityEnabled, err := resolveBool("OBSERVABILITY_ENABLED", false)
	if err != nil {
		return Config{}, invalidManagedValue("OBSERVABILITY_ENABLED")
	}
	repoRoot := resolve("ADMIN_NODE_REPO_ROOT", DefaultRepoRoot)
	adminRoot := resolve("ADMIN_NODE_ROOT", DefaultAdminRoot)
	ciMode, err := resolveBool("CI_MODE", false)
	if err != nil {
		return Config{}, invalidManagedValue("CI_MODE")
	}

	return Config{
		RepoRoot:                   repoRoot,
		AdminRoot:                  adminRoot,
		ModeFile:                   resolve("ADMIN_MODE_FILE", DefaultModeFile),
		ReleaseRefFile:             resolve("ADMIN_RELEASE_REF_FILE", DefaultReleaseRefFile),
		ReleaseNameFile:            resolve("ADMIN_RELEASE_NAME_FILE", DefaultReleaseNameFile),
		ReleaseChannelFile:         resolve("ADMIN_RELEASE_CHANNEL_FILE", DefaultReleaseChannelFile),
		QualificationFile:          resolve("ADMIN_QUALIFICATION_FILE", DefaultQualificationFile),
		GitRefFile:                 resolve("ADMIN_GIT_REF_FILE", DefaultGitRefFile),
		SchemaVersionFile:          resolve("ADMIN_CONFIG_SCHEMA_FILE", DefaultSchemaFile),
		PackageSnapshotFile:        resolve("ADMIN_PACKAGE_SNAPSHOT_FILE", DefaultPackageSnapshotFile),
		PackageSnapshotModeFile:    resolve("ADMIN_PACKAGE_SNAPSHOT_MODE_FILE", DefaultPackageSnapshotModeFile),
		RestoreIDFile:              resolve("ADMIN_RESTORE_ID_FILE", DefaultRestoreIDFile),
		BackupRoot:                 resolve("ADMIN_BACKUP_ROOT", DefaultBackupRoot),
		BackupEnvFile:              resolve("RESTIC_BACKUP_ENV_FILE", DefaultBackupEnvFile),
		OperationLock:              resolve("ADMIN_OPERATION_LOCK", DefaultOperationLock),
		GiteaStackPath:             resolve("GITEA_STACK_PATH", DefaultGiteaStackPath),
		SnapshotRoot:               resolve("ADMIN_SNAPSHOT_ROOT", DefaultSnapshotRoot),
		BackupStatusRoot:           resolve("BACKUP_STATUS_ROOT", filepath.Join(adminRoot, "backups/status")),
		RequireBtrfsHotBackup:      requireBtrfs,
		RequireHarborReadOnly:      requireHarborReadOnly,
		LocalBackupRetention:       localRetention,
		BackupOperationLockTimeout: lockTimeout,
		GiteaBackupQuiesceTimeout:  giteaQuiesceTimeout,
		OfflineBackupRetention:     offlineRetention,
		OfflineBackupMaxAge:        offlineMaxAge,
		OfflineBackupMinFreeBytes:  offlineMinFreeBytes,
		RecoveryKitInventoryFile:   resolve("BACKUP_RECOVERY_KIT_INVENTORY", "/etc/admin-node/recovery-kit-inventory.json"),
		RecoveryKitMaxAge:          recoveryKitMaxAge,
		AgeKeyFile:                 resolve("SOPS_AGE_KEY_FILE", "/etc/sops/age/keys.txt"),
		ConfigRepoRoot:             resolve("ADMIN_CONFIG_REPO_ROOT", "/etc/admin-config/homelab-node-admin-config"),
		OpenBaoRecoveryFile:        resolve("OPENBAO_RECOVERY_FILE", filepath.Join(repoRoot, "secrets/openbao-unseal.sops.yaml")),
		AdminNodeLANIP:             resolve("ADMIN_NODE_LAN_IP", DefaultAdminNodeLANIP),
		KeycloakDomain:             resolve("KEYCLOAK_DOMAIN", DefaultKeycloakDomain),
		HarborDomain:               resolve("HARBOR_DOMAIN", DefaultHarborDomain),
		GiteaDomain:                resolve("GITEA_DOMAIN", DefaultGiteaDomain),
		TraefikDomain:              resolve("TRAEFIK_DOMAIN", DefaultTraefikDomain),
		OpenBaoDomain:              resolve("OPENBAO_DOMAIN", DefaultOpenBaoDomain),
		CIMode:                     ciMode,
		CIMockPihole:               getenvBool("CI_MOCK_PIHOLE", false),
		CIMockCloudflareTunnel:     getenvBool("CI_MOCK_CLOUDFLARE_TUNNEL", false),
		PiholeDisabled:             !piholeEnabled,
		CloudflareDisabled:         !cloudflareEnabled,
		ObservabilityDisabled:      !observabilityEnabled,
		SkipPublicURLValidation:    getenvBool("SKIP_PUBLIC_URL_VALIDATION", false),
		CISkipPublicURLValidation:  getenvBool("CI_SKIP_PUBLIC_URL_VALIDATION", false),
		ValidateMockAll:            getenvBool("ADMIN_NODE_VALIDATE_MOCK_ALL", false),
		ManagedRuntimeFileLoaded:   loaded,
	}, nil
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
