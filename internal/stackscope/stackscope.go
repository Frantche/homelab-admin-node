package stackscope

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func Discover(cfg config.Config) ([]string, error) {
	root := filepath.Join(cfg.AdminRoot, "stacks")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !ValidName(entry.Name()) || disabled(cfg, entry.Name()) {
			continue
		}
		info, err := os.Stat(filepath.Join(root, entry.Name(), "compose.yaml"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if info.Mode().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func Validate(names []string) error {
	for index, name := range names {
		if !ValidName(name) {
			return fmt.Errorf("invalid active stack name %q", name)
		}
		if index > 0 && names[index-1] >= name {
			return fmt.Errorf("active stack names must be unique and sorted")
		}
	}
	return nil
}

func Contains(names []string, target string) bool {
	index := sort.SearchStrings(names, target)
	return index < len(names) && names[index] == target
}

func ValidName(name string) bool {
	return namePattern.MatchString(name)
}

func disabled(cfg config.Config, name string) bool {
	switch name {
	case "cloudflared":
		return cfg.CloudflareDisabled || cfg.CIMockCloudflareTunnel
	case "observability":
		return cfg.ObservabilityDisabled
	default:
		return false
	}
}
