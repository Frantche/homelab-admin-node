package citest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/openbao"
)

const defaultConfigRepoDir = "/etc/admin-config/homelab-node-admin-config"

type OpenBaoOptions struct {
	RootTokenOut string
	KeysetName   string
}

func InitOpenBao(ctx context.Context, cfg config.Config, opts OpenBaoOptions) error {
	secretsDir := filepath.Join(cfg.RepoRoot, "secrets")
	rootTokenOut := opts.RootTokenOut
	if rootTokenOut == "" {
		rootTokenOut = filepath.Join(secretsDir, "openbao-root-token")
	}
	keysetName := opts.KeysetName
	if keysetName == "" {
		keysetName = "ci-keyset"
	}
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return err
	}
	openbaoOpts := openbao.Options{
		SecretsDir:   secretsDir,
		KeysetName:   keysetName,
		RootTokenOut: rootTokenOut,
	}
	if err := openbao.InitIfNeeded(ctx, openbaoOpts); err != nil {
		return err
	}
	if err := openbao.Unseal(ctx, openbaoOpts); err != nil {
		return err
	}
	openbaoOpts.RootTokenFile = rootTokenOut
	return openbao.EnableKV(ctx, openbaoOpts, "secret")
}

func CreateSentinel(cfg config.Config, path string) error {
	if path == "" {
		path = filepath.Join(cfg.AdminRoot, "data/sentinel/value.txt")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	value := fmt.Sprintf("sentinel-%d\n", time.Now().Unix())
	return os.WriteFile(path, []byte(value), 0o644)
}

func InstallMockConfigRepo(cfg config.Config, source string, dest string) error {
	if source == "" {
		source = filepath.Join(cfg.RepoRoot, "ci/mock-config-repo")
	}
	if dest == "" {
		dest = getenv("CONFIG_REPO_DIR", defaultConfigRepoDir)
	}
	srcHosts := filepath.Join(source, "hosts")
	srcVars := filepath.Join(source, "group_vars/all.yml")
	for _, path := range []string{srcHosts, srcVars} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("invalid mock config source %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dest, "hosts/group_vars"), 0o755); err != nil {
		return err
	}
	if err := copyFile(srcHosts, filepath.Join(dest, "hosts/inventory.ini"), 0o644); err != nil {
		return err
	}
	if err := copyFile(srcVars, filepath.Join(dest, "hosts/group_vars/all.yml"), 0o644); err != nil {
		return err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	commands := [][]string{
		{"git", "-C", dest, "init"},
		{"git", "-C", dest, "checkout", "-B", "main"},
		{"git", "-C", dest, "add", "hosts/inventory.ini", "hosts/group_vars/all.yml"},
		{"git", "-C", dest, "-c", "user.name=CI Admin", "-c", "user.email=ci@example.com", "commit", "-m", "Initial CI admin config"},
	}
	for _, command := range commands {
		cmd := exec.CommandContext(context.Background(), command[0], command[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(out), "nothing to commit") {
			return fmt.Errorf("%s failed: %w: %s", strings.Join(command, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func UpdateOpenBaoToken(configPath string, token string, tokenFile string, ageKeyPath string) error {
	if configPath == "" {
		configPath = getenv("OPENBAO_CONFIG_PATH", filepath.Join(defaultConfigRepoDir, "hosts/group_vars/all.yml"))
	}
	if ageKeyPath == "" {
		ageKeyPath = getenv("SOPS_AGE_KEY_FILE", "/etc/sops/age/keys.txt")
	}
	if token == "" && tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		token = os.Getenv("OPENBAO_TOKEN")
	}
	if token == "" {
		return fmt.Errorf("OPENBAO_TOKEN or --token-file is required")
	}
	if err := updatePlainConfig(configPath, token); err != nil {
		return err
	}
	return updateSOPSConfig(filepath.Join(filepath.Dir(configPath), "secrets.sops.yaml"), token, ageKeyPath)
}

func updatePlainConfig(path string, token string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	for _, section := range []string{"openbao", "openbao_config"} {
		text = upsertRootToken(text, section, token)
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func upsertRootToken(text string, section string, token string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	header := section + ":"
	for idx, line := range lines {
		if line != header {
			continue
		}
		insertAt := idx + 1
		for childIdx := idx + 1; childIdx < len(lines); childIdx++ {
			childLine := lines[childIdx]
			if childLine != "" && !strings.HasPrefix(childLine, " ") {
				break
			}
			insertAt = childIdx + 1
			if strings.HasPrefix(childLine, "  root_token:") {
				lines[childIdx] = fmt.Sprintf("  root_token: %q", token)
				return strings.Join(lines, "\n") + "\n"
			}
		}
		before := append([]string{}, lines[:insertAt]...)
		after := append([]string{}, lines[insertAt:]...)
		lines = append(before, append([]string{fmt.Sprintf("  root_token: %q", token)}, after...)...)
		return strings.Join(lines, "\n") + "\n"
	}
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	lines = append(lines, header, fmt.Sprintf("  root_token: %q", token))
	return strings.Join(lines, "\n") + "\n"
}

func updateSOPSConfig(path string, token string, ageKeyPath string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Stat(ageKeyPath); err != nil {
		return fmt.Errorf("SOPS age key is required to update %s: %w", path, err)
	}
	env := append(os.Environ(), "SOPS_AGE_KEY_FILE="+ageKeyPath)
	decrypted, err := commandEnvOutput(env, "sops", "--decrypt", "--output-type", "json", path)
	if err != nil {
		return err
	}
	var data map[string]any
	if err := json.Unmarshal(decrypted, &data); err != nil {
		return err
	}
	for _, section := range []string{"openbao", "openbao_config"} {
		raw, ok := data[section]
		if !ok {
			raw = map[string]any{}
			data[section] = raw
		}
		value, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be a mapping in %s", section, path)
		}
		value["root_token"] = token
	}
	agePublic, err := commandOutput("age-keygen", "-y", ageKeyPath)
	if err != nil {
		return err
	}
	plain, err := os.CreateTemp("", "admin-openbao-token-*.json")
	if err != nil {
		return err
	}
	plainPath := plain.Name()
	defer os.Remove(plainPath)
	if err := json.NewEncoder(plain).Encode(data); err != nil {
		plain.Close()
		return err
	}
	if err := plain.Close(); err != nil {
		return err
	}
	encrypted, err := commandOutput("sops", "--config", "/dev/null", "--encrypt", "--age", strings.TrimSpace(string(agePublic)), "--input-type", "json", "--output-type", "yaml", plainPath)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0o600)
}

func copyFile(src string, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

func commandOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return out, nil
}

func commandEnvOutput(env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return out, nil
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
