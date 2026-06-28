package validate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/runner"
)

type tunnelRunner struct {
	calls int
}

func (r *tunnelRunner) Run(_ context.Context, _ string, _ ...string) runner.Result {
	r.calls++
	return runner.Result{Stdout: "cloudflared\n"}
}

type openBaoRunner struct{}

func (r openBaoRunner) Run(_ context.Context, _ string, args ...string) runner.Result {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "status -format=json"):
		return runner.Result{Stdout: `{"initialized": true, "sealed": false}`}
	case strings.Contains(joined, "kv put"):
		return runner.Result{}
	case strings.Contains(joined, "kv get"):
		return runner.Result{Stdout: "ok\n"}
	default:
		return runner.Result{}
	}
}

type observabilityRunner struct{}

func (r observabilityRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	joined := name + " " + strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "docker inspect"):
		return runner.Result{Stdout: "true\n"}
	case strings.Contains(joined, "docker exec otel-collector /otelcol-contrib --version"):
		return runner.Result{Stdout: "Server available\n"}
	default:
		return runner.Result{}
	}
}

func TestKeycloakOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/realms/master/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": "test"})
	}))
	defer server.Close()

	v := Validator{Config: config.Config{KeycloakDomain: server.URL}, Client: server.Client()}
	result := v.Keycloak(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
}

func TestHarborOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2.0/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"components": []map[string]string{{"name": "core", "status": "healthy"}},
		})
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	result := v.Harbor(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
}

func TestHarborAdminCheckWhenPasswordAvailable(t *testing.T) {
	t.Setenv("HARBOR_ADMIN_PASSWORD", "password")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2.0/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"components": []map[string]string{{"name": "core", "status": "healthy"}},
			})
		case "/api/v2.0/projects":
			user, password, ok := r.BasicAuth()
			if !ok || user != "admin" || password != "password" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]string{{"name": "library"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	result := v.Harbor(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
	if !strings.Contains(result.Message, "admin API") {
		t.Fatalf("message = %q, want admin API check", result.Message)
	}
}

func TestHarborAdminCheckFailsWithBadPassword(t *testing.T) {
	t.Setenv("HARBOR_ADMIN_PASSWORD", "bad-password")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2.0/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"components": []map[string]string{{"name": "core", "status": "healthy"}},
			})
		case "/api/v2.0/projects":
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	result := v.Harbor(context.Background())
	if result.Status != StatusFail {
		t.Fatalf("status = %s, want %s", result.Status, StatusFail)
	}
	if !strings.Contains(result.Message, "admin API check failed") {
		t.Fatalf("message = %q, want admin API failure", result.Message)
	}
}

func TestObservabilityOKWhenCollectorIsHealthy(t *testing.T) {
	v := Validator{Runner: observabilityRunner{}}
	result := v.Observability(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
}

func TestObservabilityOKWithExpectedMockContent(t *testing.T) {
	mockDir := t.TempDir()
	t.Setenv("CI_OTEL_MOCK_STATE_DIR", mockDir)
	writeMockPayload(t, mockDir, "metrics.received", `{
		"resourceMetrics": [{
			"scopeMetrics": [{
				"metrics": [{
					"data": {
						"dataPoints": [{
							"attributes": [
								{"key": "job", "value": {"stringValue": "gitea"}},
								{"key": "job", "value": {"stringValue": "harbor-core"}},
								{"key": "job", "value": {"stringValue": "harbor-exporter"}},
								{"key": "job", "value": {"stringValue": "openbao"}},
								{"key": "job", "value": {"stringValue": "traefik"}}
							]
						}]
					}
				}]
			}]
		}]
	}`)
	writeMockPayload(t, mockDir, "logs.received", `{"body":{"stringValue":"admin-node-otel-log-sentinel"}}`)

	v := Validator{Runner: observabilityRunner{}}
	result := v.Observability(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
	if !strings.Contains(result.Message, "expected service metrics") {
		t.Fatalf("message = %q, want content validation success", result.Message)
	}
}

func TestObservabilityFailsWhenMetricContentIsMissing(t *testing.T) {
	mockDir := t.TempDir()
	t.Setenv("CI_OTEL_MOCK_STATE_DIR", mockDir)
	writeMockPayload(t, mockDir, "metrics.received", "gitea harbor-core harbor-exporter openbao")
	writeMockPayload(t, mockDir, "logs.received", "admin-node-otel-log-sentinel")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := Validator{Runner: observabilityRunner{}}
	result := v.Observability(ctx)
	if result.Status != StatusFail {
		t.Fatalf("status = %s, want %s", result.Status, StatusFail)
	}
	if !strings.Contains(result.Message, "metrics content: traefik") {
		t.Fatalf("message = %q, want missing traefik metric content", result.Message)
	}
}

func TestObservabilityFailsWhenLogContentIsMissing(t *testing.T) {
	mockDir := t.TempDir()
	t.Setenv("CI_OTEL_MOCK_STATE_DIR", mockDir)
	writeMockPayload(t, mockDir, "metrics.received", "gitea harbor-core harbor-exporter openbao traefik")
	writeMockPayload(t, mockDir, "logs.received", "other log")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := Validator{Runner: observabilityRunner{}}
	result := v.Observability(ctx)
	if result.Status != StatusFail {
		t.Fatalf("status = %s, want %s", result.Status, StatusFail)
	}
	if !strings.Contains(result.Message, "logs content: admin-node-otel-log-sentinel") {
		t.Fatalf("message = %q, want missing sentinel log content", result.Message)
	}
}

func writeMockPayload(t *testing.T, dir string, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write mock payload: %v", err)
	}
}

func TestOpenBaoOKWithSentinel(t *testing.T) {
	t.Setenv("OPENBAO_TOKEN", "token")
	v := Validator{Runner: openBaoRunner{}}
	result := v.OpenBao(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
}

func TestGiteaOK(t *testing.T) {
	t.Setenv("GITEA_ADMIN_PASSWORD", "password")
	t.Setenv("GITEA_VALIDATION_CREATE", "false")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "test"})
		case "/api/v1/user":
			if _, password, ok := r.BasicAuth(); !ok || password != "password" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"login": "admin"})
		case "/api/v1/repos/admin/admin-node-validation":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":    "admin-node-validation",
				"private": true,
				"owner": map[string]string{
					"login": "admin",
				},
			})
		case "/api/v1/repos/admin/admin-node-validation/issues":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"title": "Backup restore sentinel"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{GiteaDomain: server.URL}, Client: server.Client()}
	result := v.Gitea(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
}

func TestGiteaCreatesThenRereadsSentinel(t *testing.T) {
	t.Setenv("GITEA_ADMIN_PASSWORD", "password")
	repoCreated := false
	var issues []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "test"})
		case "/api/v1/user":
			if _, password, ok := r.BasicAuth(); !ok || password != "password" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"login": "admin"})
		case "/api/v1/user/repos":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			repoCreated = true
			w.WriteHeader(http.StatusCreated)
		case "/api/v1/repos/admin/admin-node-validation":
			if !repoCreated {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":    "admin-node-validation",
				"private": true,
				"owner": map[string]string{
					"login": "admin",
				},
			})
		case "/api/v1/repos/admin/admin-node-validation/issues":
			if r.Method == http.MethodPost {
				issues = append(issues, "Backup restore sentinel")
				w.WriteHeader(http.StatusCreated)
				return
			}
			payload := make([]map[string]string, 0, len(issues))
			for _, title := range issues {
				payload = append(payload, map[string]string{"title": title})
			}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{GiteaDomain: server.URL}, Client: server.Client()}
	result := v.Gitea(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
	if !repoCreated {
		t.Fatal("expected repository to be created")
	}
	if !slices.Contains(issues, "Backup restore sentinel") {
		t.Fatal("expected sentinel issue to be created")
	}
}

func TestGiteaFailsWhenRepoIsPublic(t *testing.T) {
	t.Setenv("GITEA_ADMIN_PASSWORD", "password")
	t.Setenv("GITEA_VALIDATION_CREATE", "false")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "test"})
		case "/api/v1/user":
			_ = json.NewEncoder(w).Encode(map[string]string{"login": "admin"})
		case "/api/v1/repos/admin/admin-node-validation":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":    "admin-node-validation",
				"private": false,
				"owner": map[string]string{
					"login": "admin",
				},
			})
		case "/api/v1/repos/admin/admin-node-validation/issues":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"title": "Backup restore sentinel"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{GiteaDomain: server.URL}, Client: server.Client()}
	result := v.Gitea(context.Background())
	if result.Status != StatusFail {
		t.Fatalf("status = %s, want %s", result.Status, StatusFail)
	}
	if !strings.Contains(result.Message, "not private") {
		t.Fatalf("message = %q, want private failure", result.Message)
	}
}

func TestGiteaFailsWhenCreateDisabledAndIssueMissing(t *testing.T) {
	t.Setenv("GITEA_ADMIN_PASSWORD", "password")
	t.Setenv("GITEA_VALIDATION_CREATE", "false")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "test"})
		case "/api/v1/user":
			_ = json.NewEncoder(w).Encode(map[string]string{"login": "admin"})
		case "/api/v1/repos/admin/admin-node-validation":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":    "admin-node-validation",
				"private": true,
				"owner": map[string]string{
					"login": "admin",
				},
			})
		case "/api/v1/repos/admin/admin-node-validation/issues":
			_ = json.NewEncoder(w).Encode([]map[string]string{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{GiteaDomain: server.URL}, Client: server.Client()}
	result := v.Gitea(context.Background())
	if result.Status != StatusFail {
		t.Fatalf("status = %s, want %s", result.Status, StatusFail)
	}
	if !strings.Contains(result.Message, "validation issue not found") {
		t.Fatalf("message = %q, want missing issue failure", result.Message)
	}
}

func TestDNSMockSkipped(t *testing.T) {
	v := Validator{Config: config.Config{CIMockPihole: true}}
	result := v.DNS(context.Background())
	if result.Status != StatusSkipped {
		t.Fatalf("status = %s, want %s", result.Status, StatusSkipped)
	}
}

func TestTunnelMockOK(t *testing.T) {
	v := Validator{Config: config.Config{CIMockCloudflareTunnel: true}, Runner: &tunnelRunner{}}
	result := v.Tunnel(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
	if !strings.Contains(result.Message, "cloudflared") {
		t.Fatalf("message = %q", result.Message)
	}
}
