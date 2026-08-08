package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRepoRoot        = "/opt/homelab-admin-node"
	DefaultAdminRoot       = "/srv/admin"
	DefaultModeFile        = "/etc/admin-node/mode"
	DefaultReleaseRefFile  = "/etc/admin-node/release-ref"
	DefaultReleaseNameFile = "/etc/admin-node/release-name"
	DefaultGitRefFile      = "/etc/admin-node/git-ref"
	DefaultSchemaFile      = "/etc/admin-node/config-schema-version"
	DefaultRestoreIDFile   = "/etc/admin-node/restore-id"
	DefaultBackupRoot      = "/srv/admin/backups/local"
	DefaultBackupEnvFile   = "/srv/admin/env/backup.env"
	DefaultOperationLock   = "/run/admin-node-operation.lock"
	DefaultGiteaStackPath  = "/srv/admin/data/gitea-stack"
	DefaultSnapshotRoot    = "/srv/admin/backups/snapshots"
	DefaultAdminNodeLANIP  = "192.168.1.10"
	DefaultKeycloakDomain  = "keycloak.example.com"
	DefaultHarborDomain    = "harbor.example.com"
	DefaultGiteaDomain     = "git.example.com"
	DefaultTraefikDomain   = "traefik.example.com"
	DefaultOpenBaoDomain   = "bao.example.com"
)

type Config struct {
	RepoRoot                   string
	AdminRoot                  string
	ModeFile                   string
	ReleaseRefFile             string
	ReleaseNameFile            string
	GitRefFile                 string
	SchemaVersionFile          string
	RestoreIDFile              string
	BackupRoot                 string
	BackupEnvFile              string
	OperationLock              string
	GiteaStackPath             string
	SnapshotRoot               string
	RequireBtrfsHotBackup      bool
	RequireHarborReadOnly      bool
	LocalBackupRetention       int
	BackupOperationLockTimeout time.Duration
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
}

func FromEnv() Config {
	backupEnvFile := getenv("RESTIC_BACKUP_ENV_FILE", DefaultBackupEnvFile)
	return Config{
		RepoRoot:                   getenv("ADMIN_NODE_REPO_ROOT", DefaultRepoRoot),
		AdminRoot:                  getenv("ADMIN_NODE_ROOT", DefaultAdminRoot),
		ModeFile:                   getenv("ADMIN_MODE_FILE", DefaultModeFile),
		ReleaseRefFile:             getenv("ADMIN_RELEASE_REF_FILE", DefaultReleaseRefFile),
		ReleaseNameFile:            getenv("ADMIN_RELEASE_NAME_FILE", DefaultReleaseNameFile),
		GitRefFile:                 getenv("ADMIN_GIT_REF_FILE", DefaultGitRefFile),
		SchemaVersionFile:          getenv("ADMIN_CONFIG_SCHEMA_FILE", DefaultSchemaFile),
		RestoreIDFile:              getenv("ADMIN_RESTORE_ID_FILE", DefaultRestoreIDFile),
		BackupRoot:                 getenv("ADMIN_BACKUP_ROOT", DefaultBackupRoot),
		BackupEnvFile:              backupEnvFile,
		OperationLock:              getenv("ADMIN_OPERATION_LOCK", DefaultOperationLock),
		GiteaStackPath:             getenv("GITEA_STACK_PATH", DefaultGiteaStackPath),
		SnapshotRoot:               getenv("ADMIN_SNAPSHOT_ROOT", DefaultSnapshotRoot),
		RequireBtrfsHotBackup:      getenvBool("BACKUP_REQUIRE_BTRFS_HOT", false),
		RequireHarborReadOnly:      getenvBool("BACKUP_REQUIRE_HARBOR_READ_ONLY", false),
		LocalBackupRetention:       getenvInt("BACKUP_LOCAL_RETENTION", 3),
		BackupOperationLockTimeout: getenvDuration("BACKUP_OPERATION_LOCK_TIMEOUT", 30*time.Minute),
		AdminNodeLANIP:             getenv("ADMIN_NODE_LAN_IP", DefaultAdminNodeLANIP),
		KeycloakDomain:             getenv("KEYCLOAK_DOMAIN", DefaultKeycloakDomain),
		HarborDomain:               getenv("HARBOR_DOMAIN", DefaultHarborDomain),
		GiteaDomain:                getenv("GITEA_DOMAIN", DefaultGiteaDomain),
		TraefikDomain:              getenv("TRAEFIK_DOMAIN", DefaultTraefikDomain),
		OpenBaoDomain:              getenv("OPENBAO_DOMAIN", DefaultOpenBaoDomain),
		CIMode:                     getenvBool("CI_MODE", false),
		CIMockPihole:               getenvBool("CI_MOCK_PIHOLE", false),
		CIMockCloudflareTunnel:     getenvBool("CI_MOCK_CLOUDFLARE_TUNNEL", false),
		PiholeDisabled:             !getenvBoolFromFile("PIHOLE_ENABLED", backupEnvFile, true),
		CloudflareDisabled:         !getenvBoolFromFile("CLOUDFLARE_ENABLED", backupEnvFile, true),
		ObservabilityDisabled:      !getenvBoolFromFile("OBSERVABILITY_ENABLED", backupEnvFile, false),
		SkipPublicURLValidation:    getenvBool("SKIP_PUBLIC_URL_VALIDATION", false),
		CISkipPublicURLValidation:  getenvBool("CI_SKIP_PUBLIC_URL_VALIDATION", false),
		ValidateMockAll:            getenvBool("ADMIN_NODE_VALIDATE_MOCK_ALL", false),
	}
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
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

func getenvBoolFromFile(key, path string, fallback bool) bool {
	if _, exists := os.LookupEnv(key); exists {
		return getenvBool(key, fallback)
	}
	file, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if decoded, err := strconv.Unquote(value); err == nil {
			value = decoded
		}
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
		return fallback
	}
	return fallback
}
