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

type harborRuntimeRunner struct {
	storageCode  int
	registryCode int
}

func (r harborRuntimeRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	command := name + " " + strings.Join(args, " ")
	switch {
	case strings.Contains(command, "test -w /storage"):
		return runner.Result{Code: r.storageCode}
	case strings.Contains(command, "harbor-registry:5000/v2/_catalog"):
		return runner.Result{Code: r.registryCode}
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
	t.Setenv("HARBOR_ADMIN_PASSWORD", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2.0/health" {
			http.NotFound(w, r)
			return
		}
		writeHealthyHarborHealth(w)
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
		if r.URL.Path == "/api/v2.0/health" {
			writeHealthyHarborHealth(w)
			return
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "password" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !writeHealthyHarborAdminAPI(w, r, []string{"docker-registry", "harbor"}, "healthy") {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	result := v.Harbor(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("status = %s, want %s (%s)", result.Status, StatusOK, result.Message)
	}
	if !strings.Contains(result.Message, "Trivy report access healthy") {
		t.Fatalf("message = %q, want complete Harbor validation", result.Message)
	}
}

func TestHarborAdminCheckFailsWhenTrivyReportIsUnavailable(t *testing.T) {
	t.Setenv("HARBOR_ADMIN_PASSWORD", "password")
	t.Setenv("HARBOR_VALIDATION_SCAN_REFERENCE", "sha256:test")
	ctx, cancel := context.WithCancel(context.Background())
	scanTriggered := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "password" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:test/scan" && r.Method == http.MethodPost {
			scanTriggered = true
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if r.URL.Path == "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:test/additions/vulnerabilities" {
			cancel()
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	err := v.validateHarborScannerReport(ctx, "admin", "password")
	if err == nil || !strings.Contains(err.Error(), "Trivy vulnerability report is not accessible") {
		t.Fatalf("error = %v, want inaccessible Trivy report failure", err)
	}
	if !scanTriggered {
		t.Fatal("expected validation scan to be triggered before report check")
	}
}

func TestHarborScannerReportFallsBackToDiscoveredArtifact(t *testing.T) {
	firstScanNotFound := false
	discoveredScanTriggered := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "password" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/latest/scan":
			firstScanNotFound = true
			http.NotFound(w, r)
		case "/api/v2.0/projects/dockerhub/repositories":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "dockerhub/library/busybox", "artifact_count": 1}})
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"digest": "sha256:discovered"}})
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:discovered/scan":
			discoveredScanTriggered = true
			w.WriteHeader(http.StatusAccepted)
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:discovered/additions/vulnerabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"application/vnd.security.vulnerability.report; version=1.1": map[string]any{
					"scanner":         map[string]string{"name": "Trivy"},
					"vulnerabilities": []any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	if err := v.validateHarborScannerReport(context.Background(), "admin", "password"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !firstScanNotFound {
		t.Fatal("expected configured scan target to be attempted first")
	}
	if !discoveredScanTriggered {
		t.Fatal("expected discovered artifact digest to be scanned")
	}
}

func TestHarborScannerReportAcceptsDiscoveredArtifactWithExistingScan(t *testing.T) {
	firstScanNotFound := false
	discoveredScanAttempted := false
	reportChecked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "password" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/latest/scan":
			firstScanNotFound = true
			http.NotFound(w, r)
		case "/api/v2.0/projects/dockerhub/repositories":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "dockerhub/library/busybox", "artifact_count": 1}})
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"digest": "sha256:discovered"}})
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:discovered/scan":
			discoveredScanAttempted = true
			http.Error(w, "scan is not accepted", http.StatusBadRequest)
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:discovered/additions/vulnerabilities":
			reportChecked = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"application/vnd.security.vulnerability.report; version=1.1": map[string]any{
					"scanner":         map[string]string{"name": "Trivy"},
					"vulnerabilities": []any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	if err := v.validateHarborScannerReport(context.Background(), "admin", "password"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !firstScanNotFound {
		t.Fatal("expected configured scan target to be attempted first")
	}
	if !discoveredScanAttempted {
		t.Fatal("expected discovered artifact digest scan to be attempted")
	}
	if !reportChecked {
		t.Fatal("expected discovered artifact vulnerability report to be checked")
	}
}

func TestHarborScannerReportTriesNextDiscoveredArtifactWhenScanIsUnsupported(t *testing.T) {
	unsupportedScanAttempted := false
	supportedScanAttempted := false
	reportChecked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "password" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/latest/scan":
			http.NotFound(w, r)
		case "/api/v2.0/projects/dockerhub/repositories":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "dockerhub/library/busybox", "artifact_count": 2}})
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts":
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"digest": "sha256:unsupported"},
				{"digest": "sha256:supported"},
			})
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:unsupported/scan":
			unsupportedScanAttempted = true
			http.Error(w, "scan is not supported", http.StatusBadRequest)
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:supported/scan":
			supportedScanAttempted = true
			w.WriteHeader(http.StatusAccepted)
		case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/sha256:supported/additions/vulnerabilities":
			reportChecked = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"application/vnd.security.vulnerability.report; version=1.1": map[string]any{
					"scanner":         map[string]string{"name": "Trivy"},
					"vulnerabilities": []any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	if err := v.validateHarborScannerReport(context.Background(), "admin", "password"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !unsupportedScanAttempted {
		t.Fatal("expected unsupported discovered artifact scan to be attempted")
	}
	if !supportedScanAttempted {
		t.Fatal("expected next discovered artifact scan to be attempted")
	}
	if !reportChecked {
		t.Fatal("expected supported artifact vulnerability report to be checked")
	}
}

func TestHarborAdminCheckFailsWhenNoReplicationAdaptersAreAvailable(t *testing.T) {
	t.Setenv("HARBOR_ADMIN_PASSWORD", "password")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2.0/health" {
			writeHealthyHarborHealth(w)
			return
		}
		if !writeHealthyHarborAdminAPI(w, r, []string{}, "healthy") {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	result := v.Harbor(context.Background())
	if result.Status != StatusFail {
		t.Fatalf("status = %s, want %s", result.Status, StatusFail)
	}
	if !strings.Contains(result.Message, "no providers") {
		t.Fatalf("message = %q, want missing providers failure", result.Message)
	}
}

func TestHarborAdminCheckFailsWithoutEnabledDefaultScanner(t *testing.T) {
	t.Setenv("HARBOR_ADMIN_PASSWORD", "password")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2.0/health" {
			writeHealthyHarborHealth(w)
			return
		}
		if r.URL.Path == "/api/v2.0/scanners" {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		if !writeHealthyHarborAdminAPI(w, r, []string{"docker-registry"}, "healthy") {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	result := v.Harbor(context.Background())
	if result.Status != StatusFail || !strings.Contains(result.Message, "no enabled default scanner") {
		t.Fatalf("result = %#v, want default scanner failure", result)
	}
}

func TestHarborAdminCheckFailsWhenRegistryEndpointIsUnhealthy(t *testing.T) {
	t.Setenv("HARBOR_ADMIN_PASSWORD", "password")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2.0/health" {
			writeHealthyHarborHealth(w)
			return
		}
		if !writeHealthyHarborAdminAPI(w, r, []string{"docker-registry"}, "unhealthy") {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	result := v.Harbor(context.Background())
	if result.Status != StatusFail || !strings.Contains(result.Message, "registry endpoint dockerhub is unhealthy") {
		t.Fatalf("result = %#v, want unhealthy registry endpoint failure", result)
	}
}

func TestHarborAdminCheckFailsWithBadPassword(t *testing.T) {
	t.Setenv("HARBOR_ADMIN_PASSWORD", "bad-password")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2.0/health":
			writeHealthyHarborHealth(w)
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
	if !strings.Contains(result.Message, "projects API check failed") {
		t.Fatalf("message = %q, want admin API failure", result.Message)
	}
}

func TestHarborFailsWhenAComponentIsUnhealthy(t *testing.T) {
	t.Setenv("HARBOR_ADMIN_PASSWORD", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "unhealthy",
			"components": []map[string]string{
				{"name": "core", "status": "healthy"},
				{"name": "registry", "status": "unhealthy"},
			},
		})
	}))
	defer server.Close()

	v := Validator{Config: config.Config{HarborDomain: server.URL}, Client: server.Client()}
	result := v.Harbor(context.Background())
	if result.Status != StatusFail || !strings.Contains(result.Message, "overall health") {
		t.Fatalf("result = %#v, want overall health failure", result)
	}
}

func TestHarborRegistryRuntimeChecks(t *testing.T) {
	for _, test := range []struct {
		name         string
		runner       harborRuntimeRunner
		wantError    bool
		messageMatch string
	}{
		{name: "healthy", runner: harborRuntimeRunner{}},
		{name: "storage read only", runner: harborRuntimeRunner{storageCode: 1}, wantError: true, messageMatch: "not writable"},
		{name: "registry auth failure", runner: harborRuntimeRunner{registryCode: 1}, wantError: true, messageMatch: "authentication failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := Validator{Runner: test.runner}
			err := v.validateHarborRegistryRuntime(context.Background())
			if test.wantError && (err == nil || !strings.Contains(err.Error(), test.messageMatch)) {
				t.Fatalf("error = %v, want match %q", err, test.messageMatch)
			}
			if !test.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func writeHealthyHarborHealth(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "healthy",
		"components": []map[string]string{
			{"name": "core", "status": "healthy"},
			{"name": "database", "status": "healthy"},
			{"name": "jobservice", "status": "healthy"},
			{"name": "registry", "status": "healthy"},
		},
	})
}

func writeHealthyHarborAdminAPI(w http.ResponseWriter, r *http.Request, adapters []string, registryStatus string) bool {
	switch r.URL.Path {
	case "/api/v2.0/projects":
		_ = json.NewEncoder(w).Encode([]map[string]string{{"name": "library"}})
	case "/api/v2.0/systeminfo":
		_ = json.NewEncoder(w).Encode(map[string]any{"harbor_version": "v2.15.2", "registry_storage_provider_name": "filesystem"})
	case "/api/v2.0/systeminfo/volumes":
		_ = json.NewEncoder(w).Encode(map[string]any{"storage": []map[string]int64{{"total": 100, "free": 50}}})
	case "/api/v2.0/statistics":
		_ = json.NewEncoder(w).Encode(map[string]int{"total_project_count": 1, "total_repo_count": 1})
	case "/api/v2.0/scanners":
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "Trivy", "disabled": false, "is_default": true}})
	case "/api/v2.0/registries":
		_ = json.NewEncoder(w).Encode([]map[string]string{{"name": "dockerhub", "status": registryStatus}})
	case "/api/v2.0/replication/adapters":
		_ = json.NewEncoder(w).Encode(adapters)
	case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/latest/scan":
		if r.Method != http.MethodPost {
			return false
		}
		w.WriteHeader(http.StatusAccepted)
	case "/api/v2.0/projects/dockerhub/repositories/library%2Fbusybox/artifacts/latest/additions/vulnerabilities":
		if r.Header.Get("Accept") != "application/json" {
			http.Error(w, "missing vulnerability report accept header", http.StatusNotAcceptable)
			return true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"application/vnd.security.vulnerability.report; version=1.1": map[string]any{
				"scanner":         map[string]string{"name": "Trivy"},
				"vulnerabilities": []any{},
			},
		})
	default:
		return false
	}
	return true
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
	writeMockPayload(t, mockDir, "metrics.received", `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"gitea"}}]}},{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"harbor-core"}}]}},{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"harbor-exporter"}}]}},{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"openbao"}}]}},{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"traefik"}}]}}]}`)
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

func TestContainsFieldsLineFoldMatchesSSHDOutput(t *testing.T) {
	output := "permitrootlogin no\nloglevel verbose\nclientalivecountmax   2\n"

	for _, expected := range []string{
		"PermitRootLogin no",
		"loglevel VERBOSE",
		"clientalivecountmax 2",
	} {
		if !containsFieldsLineFold(output, expected) {
			t.Fatalf("expected %q to be found in %q", expected, output)
		}
	}
}

func TestObservabilityFailsWhenMetricContentIsMissing(t *testing.T) {
	mockDir := t.TempDir()
	t.Setenv("CI_OTEL_MOCK_STATE_DIR", mockDir)
	writeMockPayload(t, mockDir, "metrics.received", `"key":"service.name","value":{"stringValue":"gitea"} "key":"service.name","value":{"stringValue":"harbor-core"} "key":"service.name","value":{"stringValue":"harbor-exporter"} "key":"service.name","value":{"stringValue":"openbao"}`)
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
	writeMockPayload(t, mockDir, "metrics.received", `"key":"service.name","value":{"stringValue":"gitea"} "key":"service.name","value":{"stringValue":"harbor-core"} "key":"service.name","value":{"stringValue":"harbor-exporter"} "key":"service.name","value":{"stringValue":"openbao"} "key":"service.name","value":{"stringValue":"traefik"}`)
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

func TestDNSDisabledSkipped(t *testing.T) {
	v := Validator{Config: config.Config{PiholeDisabled: true}}
	result := v.DNS(context.Background())
	if result.Status != StatusSkipped {
		t.Fatalf("status = %s, want %s", result.Status, StatusSkipped)
	}
	if !strings.Contains(result.Message, "PIHOLE_ENABLED=false") {
		t.Fatalf("message = %q", result.Message)
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

func TestTunnelDisabledSkipped(t *testing.T) {
	v := Validator{Config: config.Config{CloudflareDisabled: true}, Runner: &tunnelRunner{}}
	result := v.Tunnel(context.Background())
	if result.Status != StatusSkipped {
		t.Fatalf("status = %s, want %s", result.Status, StatusSkipped)
	}
	if !strings.Contains(result.Message, "CLOUDFLARE_ENABLED=false") {
		t.Fatalf("message = %q", result.Message)
	}
}
