package ci

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stateful API double: the real Ansible tasks render policies and reconcile
// roles/credentials. Only OpenBao HTTP responses are simulated.
type approleMock struct {
	mu                  sync.Mutex
	backend             string
	policies            map[string]string
	roles               map[string]map[string]any
	credentials         map[string]map[string]any
	secrets             map[string]map[string]any
	writes, generations int
	rejectLookup        bool
}

func newApproleMock() *approleMock {
	return &approleMock{policies: map[string]string{}, roles: map[string]map[string]any{}, credentials: map[string]map[string]any{}, secrets: map[string]map[string]any{}}
}
func (m *approleMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Header.Get("X-Vault-Token") != "ci-approle-admin-token" {
		writeJSON(w, 403, map[string]any{})
		return
	}
	path := r.URL.Path
	data := func(value any) { writeJSON(w, 200, map[string]any{"data": value}) }
	missing := func() { writeJSON(w, 404, map[string]any{"errors": []string{}}) }
	var body map[string]any
	if r.Method == "POST" || r.Method == "PUT" {
		decodeJSON(r, &body)
	}
	switch {
	case path == "/v1/sys/auth" && r.Method == "GET":
		auth := map[string]any{}
		if m.backend != "" {
			auth["approle/"] = map[string]any{"type": m.backend}
		}
		data(auth)
	case path == "/v1/sys/auth/approle" && r.Method == "POST":
		if m.backend != "" {
			writeJSON(w, 400, map[string]any{})
			return
		}
		m.backend = body["type"].(string)
		m.writes++
		w.WriteHeader(204)
	case strings.HasPrefix(path, "/v1/sys/policies/acl/"):
		name := strings.TrimPrefix(path, "/v1/sys/policies/acl/")
		if r.Method == "GET" {
			if policy, ok := m.policies[name]; ok {
				data(map[string]any{"policy": policy})
			} else {
				missing()
			}
			return
		}
		if r.Method != "PUT" {
			w.WriteHeader(405)
			return
		}
		m.policies[name] = body["policy"].(string)
		m.writes++
		w.WriteHeader(204)
	case strings.HasPrefix(path, "/v1/auth/approle/role/"):
		parts := strings.SplitN(strings.TrimPrefix(path, "/v1/auth/approle/role/"), "/", 2)
		name := parts[0]
		suffix := ""
		if len(parts) == 2 {
			suffix = parts[1]
		}
		if suffix == "" && r.Method == "POST" {
			m.roles[name] = body
			m.writes++
			w.WriteHeader(204)
			return
		}
		role, ok := m.roles[name]
		if !ok {
			missing()
			return
		}
		switch suffix {
		case "":
			data(role)
		case "role-id":
			data(map[string]any{"role_id": "ci-role-" + name})
		case "secret-id":
			m.generations++
			m.writes++
			secret := fmt.Sprintf("ci-generated-secret-%d", m.generations)
			value := map[string]any{"secret_id_accessor": fmt.Sprintf("ci-accessor-%d", m.generations), "secret_id_ttl": float64(0), "secret_id_num_uses": float64(0)}
			m.secrets[name+"/"+secret] = value
			result := cloneMap(value)
			result["secret_id"] = secret
			data(result)
		case "secret-id/lookup":
			if m.rejectLookup {
				writeJSON(w, 403, map[string]any{"errors": []string{"permission denied"}})
				return
			}
			if value, ok := m.secrets[name+"/"+body["secret_id"].(string)]; ok {
				data(value)
			} else {
				writeJSON(w, 400, map[string]any{"errors": []string{"invalid secret id"}})
			}
		default:
			missing()
		}
	case strings.HasPrefix(path, "/v1/secret/data/"):
		if r.Method == "GET" {
			if value, ok := m.credentials[path]; ok {
				data(map[string]any{"data": value})
			} else {
				missing()
			}
			return
		}
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		m.credentials[path] = body["data"].(map[string]any)
		m.writes++
		data(map[string]any{"version": 1})
	default:
		missing()
	}
}
func approleDeclaration(name string) map[string]any {
	return map[string]any{"name": name, "secret_engines": []string{"secret", "homelab"}, "rotation_id": "v1", "openbao": map[string]any{"mount": "secret", "path": "approles/" + name}}
}
func runApproleContract(t *testing.T, url string, roles []map[string]any, wantSuccess, validateCerts bool) string {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(source))
	vars := map[string]any{"mock_base_url": url, "openbao_config": map[string]any{"approles": roles}, "contract_validate_certs": validateCerts}
	payload, err := json.Marshal(vars)
	if err != nil {
		t.Fatal(err)
	}
	varsPath := filepath.Join(t.TempDir(), "vars.json")
	if err := os.WriteFile(varsPath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ansible-playbook", "-i", "localhost,", filepath.Join(root, "ci/playbooks/openbao-approles-contracts.yml"), "-e", "@"+varsPath)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "ANSIBLE_ROLES_PATH="+filepath.Join(root, "ansible/roles"), "ANSIBLE_NOCOLOR=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("AppRole playbook timeout: %s", output)
	}
	if (err == nil) != wantSuccess {
		t.Fatalf("AppRole playbook success=%v, want %v: %s", err == nil, wantSuccess, output)
	}
	for _, secret := range []string{"ci-generated-secret-", "ci-approle-admin-token"} {
		if strings.Contains(string(output), secret) {
			t.Fatal("Ansible exposed a confidential mock credential")
		}
	}
	return string(output)
}
func assertApproleReadPolicy(t *testing.T, policy string) {
	t.Helper()
	pattern := regexp.MustCompile(`(?s)path "([^"]+)"\s*\{\s*capabilities = \[([^\]]+)\]\s*\}`)
	rules := pattern.FindAllStringSubmatch(policy, -1)
	want := map[string]string{"secret/data/*": "\"read\"", "homelab/data/*": "\"read\"", "secret/metadata": "\"list\"", "homelab/metadata": "\"list\"", "secret/metadata/*": "\"read\", \"list\"", "homelab/metadata/*": "\"read\", \"list\""}
	if len(rules) != len(want) || strings.TrimSpace(pattern.ReplaceAllString(policy, "")) != "" {
		t.Fatalf("unexpected policy structure: %s", policy)
	}
	for _, rule := range rules {
		if expected, ok := want[rule[1]]; !ok || strings.TrimSpace(rule[2]) != expected {
			t.Fatalf("unexpected privilege in %s", rule[0])
		}
		delete(want, rule[1])
	}
	if len(want) != 0 {
		t.Fatalf("missing read/list rules: %v", want)
	}
}
func TestOpenBaoAppRolesLifecycle(t *testing.T) {
	mock := newApproleMock()
	server := httptest.NewServer(mock)
	defer server.Close()
	roles := []map[string]any{approleDeclaration("talos-homelab"), approleDeclaration("second-cluster")}
	roles[1]["token_ttl"] = 600
	roles[1]["token_max_ttl"] = 1200
	run := func() { runApproleContract(t, server.URL, roles, true, false) }
	key := "/v1/secret/data/approles/talos-homelab"
	run()
	mock.mu.Lock()
	if mock.generations != 2 || len(mock.roles) != 2 {
		t.Fatal("expected two independently generated roles")
	}
	for _, policy := range mock.policies {
		assertApproleReadPolicy(t, policy)
	}
	first := cloneMap(mock.credentials[key])
	writes := mock.writes
	role := mock.roles["talos-homelab"]
	for field, want := range map[string]any{"bind_secret_id": true, "secret_id_ttl": float64(0), "secret_id_num_uses": float64(0), "token_ttl": float64(3600), "token_max_ttl": float64(14400), "token_explicit_max_ttl": float64(14400)} {
		if role[field] != want {
			t.Fatalf("%s=%v, want %v", field, role[field], want)
		}
	}
	if !reflect.DeepEqual(role["token_policies"], []any{"approle-talos-homelab"}) {
		t.Fatal("unexpected token policies")
	}
	if mock.roles["second-cluster"]["token_ttl"] != float64(600) {
		t.Fatal("custom TTL ignored")
	}
	mock.mu.Unlock()
	output := runApproleContract(t, server.URL, roles, true, false)
	mock.mu.Lock()
	if mock.writes != writes || !reflect.DeepEqual(first, mock.credentials[key]) || !strings.Contains(output, "changed=0") {
		t.Fatal("second convergence mutated stable state")
	}
	mock.policies["approle-talos-homelab"] = `path "*" { capabilities = ["sudo"] }`
	mock.roles["talos-homelab"]["token_policies"] = []any{"admin"}
	mock.mu.Unlock()
	run()
	mock.mu.Lock()
	assertApproleReadPolicy(t, mock.policies["approle-talos-homelab"])
	if mock.writes != writes+2 || mock.generations != 2 {
		t.Fatal("drift must update only role and policy")
	}
	mock.mu.Unlock()
	roles[0]["rotation_id"] = "v2"
	run()
	mock.mu.Lock()
	rotated := cloneMap(mock.credentials[key])
	if rotated["secret_id"] == first["secret_id"] || rotated["rotation_id"] != "v2" || !reflect.DeepEqual(rotated["previous_secret_id_accessors"], []any{first["secret_id_accessor"]}) {
		t.Fatal("rotation did not publish a replacement and retain the accessor")
	}
	if mock.secrets["talos-homelab/"+first["secret_id"].(string)] == nil {
		t.Fatal("rotation revoked credentials before consumer migration")
	}
	delete(mock.secrets, "talos-homelab/"+rotated["secret_id"].(string))
	mock.mu.Unlock()
	run()
	mock.mu.Lock()
	if mock.credentials[key]["secret_id"] == rotated["secret_id"] {
		t.Fatal("revoked SecretID was reused")
	}
	delete(mock.credentials, key)
	mock.mu.Unlock()
	run()
	mock.mu.Lock()
	if mock.credentials[key] == nil || mock.generations != 5 {
		t.Fatal("missing credentials were not recovered")
	}
	current := mock.credentials[key]["secret_id"].(string)
	mock.secrets["talos-homelab/"+current]["secret_id_ttl"] = float64(60)
	mock.mu.Unlock()
	run()
	mock.mu.Lock()
	if mock.generations != 6 {
		t.Fatal("finite lifetime SecretID was reused")
	}
	mock.rejectLookup = true
	writes = mock.writes
	mock.mu.Unlock()
	runApproleContract(t, server.URL, roles, false, false)
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.writes != writes {
		t.Fatal("permission failure must not trigger rotation or publication")
	}
}
func TestOpenBaoAppRolesInvalidDeclarations(t *testing.T) {
	cases := []struct {
		name    string
		modify  func([]map[string]any) []map[string]any
		backend string
	}{
		{"duplicate name", func(r []map[string]any) []map[string]any { return append(r, approleDeclaration("talos-homelab")) }, ""},
		{"duplicate destination", func(r []map[string]any) []map[string]any {
			other := approleDeclaration("other")
			other["openbao"] = r[0]["openbao"]
			return append(r, other)
		}, ""},
		{"unsafe name", func(r []map[string]any) []map[string]any { r[0]["name"] = "../admin"; return r }, ""},
		{"undeclared engine", func(r []map[string]any) []map[string]any { r[0]["secret_engines"] = []string{"unknown"}; return r }, ""},
		{"empty engines", func(r []map[string]any) []map[string]any { r[0]["secret_engines"] = []string{}; return r }, ""},
		{"unsafe destination", func(r []map[string]any) []map[string]any {
			r[0]["openbao"].(map[string]any)["path"] = "../admin"
			return r
		}, ""},
		{"missing rotation", func(r []map[string]any) []map[string]any { delete(r[0], "rotation_id"); return r }, ""},
		{"invalid TTL", func(r []map[string]any) []map[string]any { r[0]["token_ttl"] = 0; return r }, ""},
		{"inverted TTL", func(r []map[string]any) []map[string]any { r[0]["token_max_ttl"] = 30; return r }, ""},
		{"conflicting backend", func(r []map[string]any) []map[string]any { return r }, "oidc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := newApproleMock()
			mock.backend = tc.backend
			server := httptest.NewServer(mock)
			defer server.Close()
			runApproleContract(t, server.URL, tc.modify([]map[string]any{approleDeclaration("talos-homelab")}), false, false)
			mock.mu.Lock()
			defer mock.mu.Unlock()
			if mock.writes != 0 {
				t.Fatal("invalid config caused API mutations")
			}
		})
	}
}
func TestOpenBaoAppRolesRejectUntrustedTLS(t *testing.T) {
	for _, backend := range []string{"", "approle"} {
		name := "enable-backend"
		if backend != "" {
			name = "reconcile-role"
		}
		t.Run(name, func(t *testing.T) {
			mock := newApproleMock()
			mock.backend = backend
			server := httptest.NewTLSServer(mock)
			defer server.Close()
			runApproleContract(t, server.URL, []map[string]any{approleDeclaration("talos-homelab")}, false, true)
			mock.mu.Lock()
			defer mock.mu.Unlock()
			if mock.writes != 0 {
				t.Fatal("untrusted server received writes")
			}
		})
	}
}
