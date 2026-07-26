package stackscope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

func TestDiscoverFiltersDisabledAndMockStacks(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"cloudflared", "future_stack", "gitea", "observability"} {
		dir := filepath.Join(root, "stacks", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	names, err := Discover(config.Config{
		AdminRoot:              root,
		CIMockCloudflareTunnel: true,
		ObservabilityDisabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, ","); got != "future_stack,gitea" {
		t.Fatalf("active stacks = %q", got)
	}
}

func TestDiscoverIncludesEnabledOptionalStacks(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"cloudflared", "observability"} {
		dir := filepath.Join(root, "stacks", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	names, err := Discover(config.Config{AdminRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, ","); got != "cloudflared,observability" {
		t.Fatalf("active stacks = %q", got)
	}
}
