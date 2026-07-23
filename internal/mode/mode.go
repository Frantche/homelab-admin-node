package mode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var validModes = map[string]bool{
	"locked":         true,
	"init":           true,
	"normal":         true,
	"restore":        true,
	"restore_failed": true,
}

func Read(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if !validModes[value] {
		return "", fmt.Errorf("invalid mode: %q", value)
	}
	return value, nil
}

func IsValid(value string) bool {
	return validModes[strings.TrimSpace(value)]
}

func Set(path, value string) error {
	value = strings.TrimSpace(value)
	if !validModes[value] {
		return fmt.Errorf("invalid mode: %s", value)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mode-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(value + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
