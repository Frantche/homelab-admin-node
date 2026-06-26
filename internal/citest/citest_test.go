package citest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

func TestCreateSentinel(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{AdminRoot: root}

	if err := CreateSentinel(cfg, ""); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "data/sentinel/value.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "sentinel-") {
		t.Fatalf("sentinel content = %q", string(content))
	}
}

func TestInstallMockConfigRepo(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(source, "group_vars"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "hosts"), []byte("localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "group_vars/all.yml"), []byte("ci_mode: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallMockConfigRepo(config.Config{}, source, dest); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(dest, "hosts/inventory.ini")); err != nil || string(content) != "localhost\n" {
		t.Fatalf("inventory content = %q, err=%v", string(content), err)
	}
	if content, err := os.ReadFile(filepath.Join(dest, "hosts/group_vars/all.yml")); err != nil || string(content) != "ci_mode: true\n" {
		t.Fatalf("vars content = %q, err=%v", string(content), err)
	}
}

func TestUpdateOpenBaoTokenPlainConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "all.yml")
	input := `openbao:
  root_token: ""
  other: true
openbao_config:
  enabled: true
`
	if err := os.WriteFile(configPath, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateOpenBaoToken(configPath, "token-value", "", ""); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, expected := range []string{
		"openbao:\n  root_token: \"token-value\"\n  other: true",
		"openbao_config:\n  enabled: true\n  root_token: \"token-value\"",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("updated config missing %q:\n%s", expected, text)
		}
	}
}
