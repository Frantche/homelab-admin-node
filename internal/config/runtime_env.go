package config

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

var managedRuntimeKeys = map[string]struct{}{
	"CI_MODE":                         {},
	"ADMIN_NODE_REPO_ROOT":            {},
	"ADMIN_NODE_ROOT":                 {},
	"ADMIN_BACKUP_ROOT":               {},
	"ADMIN_MODE_FILE":                 {},
	"ADMIN_RESTORE_ID_FILE":           {},
	"ADMIN_NODE_LAN_IP":               {},
	"ADMIN_OPERATION_LOCK":            {},
	"ADMIN_SNAPSHOT_ROOT":             {},
	"BACKUP_STATUS_ROOT":              {},
	"KEYCLOAK_DOMAIN":                 {},
	"HARBOR_DOMAIN":                   {},
	"GITEA_DOMAIN":                    {},
	"TRAEFIK_DOMAIN":                  {},
	"OPENBAO_DOMAIN":                  {},
	"PIHOLE_ENABLED":                  {},
	"CLOUDFLARE_ENABLED":              {},
	"OBSERVABILITY_ENABLED":           {},
	"BACKUP_OPERATION_LOCK_TIMEOUT":   {},
	"BACKUP_GITEA_QUIESCE_TIMEOUT":    {},
	"BACKUP_REQUIRE_BTRFS_HOT":        {},
	"BACKUP_REQUIRE_HARBOR_READ_ONLY": {},
	"BACKUP_LOCAL_RETENTION":          {},
	"GITEA_STACK_PATH":                {},
}

func readManagedRuntimeFile(path string) (map[string]string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, false, nil
		}
		return nil, false, fmt.Errorf("read managed runtime configuration %s: %w", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, rawValue, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || !validEnvironmentName(name) {
			return nil, true, fmt.Errorf("parse managed runtime configuration %s:%d: expected KEY=VALUE", path, lineNumber)
		}
		if _, allowed := managedRuntimeKeys[name]; !allowed {
			continue
		}
		value, err := decodeEnvironmentValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, true, fmt.Errorf("parse managed runtime configuration %s:%d for %s: %w", path, lineNumber, name, err)
		}
		values[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, true, fmt.Errorf("read managed runtime configuration %s: %w", path, err)
	}
	return values, true, nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '_' || (index > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func decodeEnvironmentValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "\"") {
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid double-quoted value")
		}
		return decoded, nil
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return "", fmt.Errorf("invalid single-quoted value")
		}
		return value[1 : len(value)-1], nil
	}
	if strings.ContainsAny(value, "\"'") {
		return "", fmt.Errorf("unexpected quote in unquoted value")
	}
	return value, nil
}

func invalidManagedValue(key string) error {
	return fmt.Errorf("invalid runtime configuration value for %s", key)
}

func (cfg Config) ValidateOperational() error {
	if cfg.CIMode || cfg.ValidateMockAll {
		return nil
	}
	required := map[string]string{
		"KEYCLOAK_DOMAIN": cfg.KeycloakDomain,
		"HARBOR_DOMAIN":   cfg.HarborDomain,
		"GITEA_DOMAIN":    cfg.GiteaDomain,
		"TRAEFIK_DOMAIN":  cfg.TraefikDomain,
		"OPENBAO_DOMAIN":  cfg.OpenBaoDomain,
	}
	var missing []string
	for key, value := range required {
		if value == "" || strings.HasSuffix(value, ".example.com") {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("managed runtime configuration is incomplete: %s still use built-in example values; run convergence or set explicit process environment overrides", strings.Join(missing, ", "))
}
