package openbao

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const redactedValue = "[REDACTED]"

var secretAssignment = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:TOKEN|PASSWORD|SECRET|KEY)[A-Z0-9_]*)=([^\s]+)`)

type Options struct {
	AgeKey        string
	SecretsDir    string
	SecretsFile   string
	KeysetName    string
	Container     string
	RootTokenOut  string
	RootToken     string
	RootTokenFile string
}

type status struct {
	Initialized bool `json:"initialized"`
	Sealed      bool `json:"sealed"`
}

func InitIfNeeded(ctx context.Context, opts Options) error {
	opts = defaults(opts)
	if _, err := os.Stat(opts.AgeKey); err != nil {
		return fmt.Errorf("missing age private key at %s", opts.AgeKey)
	}
	st, err := waitStatus(ctx, opts.Container)
	if err != nil {
		return err
	}
	secretsExist := fileExists(opts.SecretsFile)
	if st.Initialized {
		if secretsExist {
			fmt.Printf("OpenBao unseal secrets already exist: %s\n", opts.SecretsFile)
			return nil
		}
		return fmt.Errorf("OpenBao is already initialized but %s is missing", opts.SecretsFile)
	}
	initJSON, err := dockerOutput(ctx, opts.Container, "bao", "operator", "init", "-key-shares=5", "-key-threshold=3", "-format=json")
	if err != nil {
		return err
	}
	var initData map[string]any
	if err := json.Unmarshal(initJSON, &initData); err != nil {
		return err
	}
	keys := initKeys(initData)
	if len(keys) == 0 {
		return fmt.Errorf("no unseal keys found in init output")
	}
	rootToken, _ := initData["root_token"].(string)
	plain := renderSecrets(opts.KeysetName, keys, rootToken)
	agePublic, err := commandOutput(ctx, "", "age-keygen", "-y", opts.AgeKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opts.SecretsDir, 0o700); err != nil {
		return err
	}
	encrypted, err := commandInputOutput(ctx, plain, "sops", "--config", "/dev/null", "--encrypt", "--age", strings.TrimSpace(string(agePublic)), "--input-type", "yaml", "--output-type", "yaml", "--filename-override", opts.SecretsFile, "/dev/stdin")
	if err != nil {
		return err
	}
	if secretsExist {
		backupPath, err := backupExistingFile(opts.SecretsFile)
		if err != nil {
			return err
		}
		fmt.Printf("[openbao-init] backed up existing unseal secrets to %s\n", backupPath)
	}
	if err := os.WriteFile(opts.SecretsFile, encrypted, 0o600); err != nil {
		return err
	}
	if opts.RootTokenOut != "" {
		if err := os.WriteFile(opts.RootTokenOut, []byte(rootToken+"\n"), 0o600); err != nil {
			return err
		}
	}
	fmt.Printf("[openbao-init] wrote encrypted unseal secrets to %s\n", opts.SecretsFile)
	return nil
}

func backupExistingFile(path string) (string, error) {
	input, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	backupPath := fmt.Sprintf("%s.preinit-backup-%s", path, time.Now().UTC().Format("20060102-150405"))
	if err := os.WriteFile(backupPath, input, 0o600); err != nil {
		return "", err
	}
	return backupPath, nil
}

func Unseal(ctx context.Context, opts Options) error {
	opts = defaults(opts)
	st, err := waitStatus(ctx, opts.Container)
	if err != nil {
		return err
	}
	if !st.Initialized {
		if !fileExists(opts.SecretsFile) {
			fmt.Println("OpenBao is not initialized yet; skipping unseal")
			return nil
		}
		st, err = waitInitialized(ctx, opts.Container)
		if err != nil {
			return err
		}
	}
	if !st.Sealed {
		fmt.Println("OpenBao already unsealed")
		return nil
	}
	if _, err := os.Stat(opts.AgeKey); err != nil {
		return fmt.Errorf("missing age private key at %s", opts.AgeKey)
	}
	decrypted, err := commandEnvOutput(ctx, []string{"SOPS_AGE_KEY_FILE=" + opts.AgeKey}, "sops", "--decrypt", "--output-type", "json", opts.SecretsFile)
	if err != nil {
		return err
	}
	var data struct {
		OpenBao struct {
			ActiveKeyset string `json:"active_keyset"`
			Keysets      map[string]struct {
				Threshold  int      `json:"threshold"`
				UnsealKeys []string `json:"unseal_keys"`
			} `json:"keysets"`
		} `json:"openbao"`
	}
	if err := json.Unmarshal(decrypted, &data); err != nil {
		return err
	}
	keyset, ok := data.OpenBao.Keysets[data.OpenBao.ActiveKeyset]
	if !ok {
		return fmt.Errorf("active OpenBao keyset not found")
	}
	if keyset.Threshold <= 0 || len(keyset.UnsealKeys) < keyset.Threshold {
		return fmt.Errorf("not enough OpenBao unseal keys for threshold")
	}
	for _, key := range keyset.UnsealKeys[:keyset.Threshold] {
		_, _ = dockerOutput(ctx, opts.Container, "bao", "operator", "unseal", key)
	}
	st, err = getStatus(ctx, opts.Container)
	if err != nil {
		return err
	}
	if st.Sealed {
		return fmt.Errorf("OpenBao unseal failed")
	}
	fmt.Println("OpenBao unsealed successfully")
	return nil
}

func EnableKV(ctx context.Context, opts Options, path string) error {
	opts = defaults(opts)
	token, err := rootToken(opts)
	if err != nil {
		return err
	}
	path = strings.Trim(path, "/")
	if path == "" {
		path = "secret"
	}
	list, err := dockerOutputEnv(ctx, opts.Container, []string{"VAULT_TOKEN=" + token}, "bao", "secrets", "list", "-format=json")
	if err == nil && strings.Contains(string(list), `"`+path+`/"`) {
		return nil
	}
	if _, err := dockerOutputEnv(ctx, opts.Container, []string{"VAULT_TOKEN=" + token}, "bao", "secrets", "enable", "-path="+path, "kv-v2"); err != nil {
		list, listErr := dockerOutputEnv(ctx, opts.Container, []string{"VAULT_TOKEN=" + token}, "bao", "secrets", "list", "-format=json")
		if listErr == nil && strings.Contains(string(list), `"`+path+`/"`) {
			return nil
		}
		return err
	}
	return nil
}

func rootToken(opts Options) (string, error) {
	if opts.RootToken != "" {
		return opts.RootToken, nil
	}
	if token := os.Getenv("OPENBAO_TOKEN"); token != "" {
		return token, nil
	}
	if opts.RootTokenFile != "" {
		data, err := os.ReadFile(opts.RootTokenFile)
		if err != nil {
			return "", err
		}
		token := strings.TrimSpace(string(data))
		if token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("OPENBAO_TOKEN or --token-file is required")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func defaults(opts Options) Options {
	if opts.AgeKey == "" {
		opts.AgeKey = "/etc/sops/age/keys.txt"
	}
	if opts.SecretsDir == "" {
		opts.SecretsDir = "/opt/homelab-admin-node/secrets"
	}
	if opts.SecretsFile == "" {
		opts.SecretsFile = filepath.Join(opts.SecretsDir, "openbao-unseal.sops.yaml")
	}
	if opts.KeysetName == "" {
		opts.KeysetName = time.Now().Format("2006-01-initial")
	}
	if opts.Container == "" {
		opts.Container = "openbao"
	}
	return opts
}

func waitStatus(ctx context.Context, container string) (status, error) {
	var last error
	for range 60 {
		st, err := getStatus(ctx, container)
		if err == nil {
			return st, nil
		}
		last = err
		time.Sleep(2 * time.Second)
	}
	return status{}, fmt.Errorf("OpenBao did not become reachable: %w", last)
}

func waitInitialized(ctx context.Context, container string) (status, error) {
	var last status
	for range 60 {
		st, err := waitStatus(ctx, container)
		if err != nil {
			return status{}, err
		}
		if st.Initialized {
			return st, nil
		}
		last = st
		time.Sleep(2 * time.Second)
	}
	return last, fmt.Errorf("OpenBao did not report initialized before unseal")
}

func getStatus(ctx context.Context, container string) (status, error) {
	out, err := dockerStatusOutput(ctx, container)
	if err != nil {
		return status{}, err
	}
	var st status
	return st, json.Unmarshal(out, &st)
}

func initKeys(data map[string]any) []string {
	for _, field := range []string{"unseal_keys_b64", "unseal_keys_hex", "unseal_keys", "keys_base64", "keys"} {
		raw, ok := data[field].([]any)
		if !ok {
			continue
		}
		var keys []string
		for _, item := range raw {
			if key, ok := item.(string); ok {
				keys = append(keys, key)
			}
		}
		return keys
	}
	return nil
}

func renderSecrets(keyset string, keys []string, rootToken string) []byte {
	var b strings.Builder
	fmt.Fprintln(&b, "openbao:")
	fmt.Fprintf(&b, "  active_keyset: %q\n", keyset)
	fmt.Fprintln(&b, "  keysets:")
	fmt.Fprintf(&b, "    %q:\n", keyset)
	fmt.Fprintln(&b, "      threshold: 3")
	fmt.Fprintln(&b, "      unseal_keys:")
	for _, key := range keys {
		fmt.Fprintf(&b, "        - %q\n", key)
	}
	fmt.Fprintf(&b, "  root_token: %q\n", rootToken)
	fmt.Fprintln(&b, "openbao_config:")
	fmt.Fprintf(&b, "  root_token: %q\n", rootToken)
	return []byte(b.String())
}

func dockerOutput(ctx context.Context, container string, args ...string) ([]byte, error) {
	base := []string{"exec", "-e", "BAO_ADDR=https://127.0.0.1:8200", "-e", "BAO_CACERT=/openbao/tls/ca.pem", container}
	return commandOutput(ctx, "", "docker", append(base, args...)...)
}

func dockerStatusOutput(ctx context.Context, container string) ([]byte, error) {
	args := []string{"exec", "-e", "BAO_ADDR=https://127.0.0.1:8200", "-e", "BAO_CACERT=/openbao/tls/ca.pem", container, "bao", "status", "-format=json"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 && json.Valid(out) {
		return out, nil
	}
	return nil, commandError("docker", args, err, out, nil, false)
}

func dockerOutputEnv(ctx context.Context, container string, env []string, args ...string) ([]byte, error) {
	base := []string{"exec", "-e", "BAO_ADDR=https://127.0.0.1:8200", "-e", "BAO_CACERT=/openbao/tls/ca.pem"}
	for _, item := range env {
		name, _, _ := strings.Cut(item, "=")
		base = append(base, "-e", name)
	}
	base = append(base, container)
	return commandEnvOutput(ctx, env, "docker", append(base, args...)...)
}

func commandOutput(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, commandError(name, args, err, out, nil, commandHasSensitiveOutput(args))
	}
	return out, nil
}

func commandEnvOutput(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, commandError(name, args, err, out, secretEnvValues(env), commandHasSensitiveOutput(args))
	}
	return out, nil
}

func commandInputOutput(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Commands receiving secret material on stdin must not be allowed to echo
		// that material back into an error.
		return nil, commandError(name, args, err, out, nil, len(input) > 0)
	}
	return out, nil
}

func commandError(name string, args []string, err error, out []byte, secrets []string, suppressOutput bool) error {
	safeArgs, argSecrets := redactCommandArgs(args)
	secrets = append(secrets, argSecrets...)
	safeOutput := strings.TrimSpace(string(out))
	if suppressOutput && safeOutput != "" {
		safeOutput = redactedValue
	} else {
		for _, secret := range secrets {
			if secret != "" {
				safeOutput = strings.ReplaceAll(safeOutput, secret, redactedValue)
			}
		}
		safeOutput = secretAssignment.ReplaceAllString(safeOutput, "${1}="+redactedValue)
	}
	if safeOutput == "" {
		return fmt.Errorf("%s %s: %w", name, strings.Join(safeArgs, " "), err)
	}
	return fmt.Errorf("%s %s: %w: %s", name, strings.Join(safeArgs, " "), err, safeOutput)
}

func redactCommandArgs(args []string) ([]string, []string) {
	safe := append([]string(nil), args...)
	var secrets []string
	for i, arg := range safe {
		if i >= 2 && safe[i-2] == "operator" && safe[i-1] == "unseal" {
			secrets = append(secrets, arg)
			safe[i] = redactedValue
			continue
		}
		name, value, found := strings.Cut(arg, "=")
		if found && sensitiveName(name) {
			secrets = append(secrets, value)
			safe[i] = name + "=" + redactedValue
		}
	}
	return safe, secrets
}

func secretEnvValues(env []string) []string {
	var secrets []string
	for _, item := range env {
		name, value, found := strings.Cut(item, "=")
		if found && sensitiveName(name) {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func sensitiveName(name string) bool {
	upper := strings.ToUpper(name)
	return strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "SECRET") ||
		strings.HasSuffix(upper, "_KEY")
}

func commandHasSensitiveOutput(args []string) bool {
	for i := range args {
		if i >= 2 && args[i-2] == "bao" && args[i-1] == "operator" && args[i] == "init" {
			return true
		}
	}
	return false
}
