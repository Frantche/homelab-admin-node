package validate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "admin-node-validation"})
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
