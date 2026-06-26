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

func Set(path, value string) error {
	value = strings.TrimSpace(value)
	if !validModes[value] {
		return fmt.Errorf("invalid mode: %s", value)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value+"\n"), 0o644)
}
