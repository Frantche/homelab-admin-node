package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/runner"
)

type Validator struct {
	Config config.Config
	Runner runner.Runner
	Client HTTPDoer
}

func NewValidator(cfg config.Config, run runner.Runner) Validator {
	if run == nil {
		run = runner.ExecRunner{}
	}
	return Validator{Config: cfg, Runner: run, Client: defaultHTTPClient()}
}

func (v Validator) APIS(ctx context.Context) []CheckResult {
	return []CheckResult{
		v.OpenBao(ctx),
		v.Keycloak(ctx),
		v.Harbor(ctx),
		v.Gitea(ctx),
		v.Traefik(ctx),
	}
}

func (v Validator) All(ctx context.Context) []CheckResult {
	results := v.APIS(ctx)
	results = append(results, v.DNS(ctx), v.Tunnel(ctx))
	return results
}

func (v Validator) Keycloak(ctx context.Context) CheckResult {
	return timed("Keycloak", func() (Status, string) {
		if v.Config.ValidateMockAll {
			return StatusSkipped, "ADMIN_NODE_VALIDATE_MOCK_ALL=true"
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		rawURL := serviceURL(v.Config.KeycloakDomain, "/realms/master/.well-known/openid-configuration")
		for {
			req, err := httpRequest(ctx, rawURL)
			if err == nil {
				resp, err := v.Client.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
						return StatusOK, "realm discovery reachable"
					}
				}
			}
			if ctx.Err() != nil {
				return StatusFail, "realm discovery unreachable"
			}
			time.Sleep(3 * time.Second)
		}
	})
}

func (v Validator) Harbor(ctx context.Context) CheckResult {
	return timed("Harbor", func() (Status, string) {
		if v.Config.ValidateMockAll {
			return StatusSkipped, "ADMIN_NODE_VALIDATE_MOCK_ALL=true"
		}
		ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		rawURL := serviceURL(v.Config.HarborDomain, "/api/v2.0/health")
		type component struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		var health struct {
			Components []component `json:"components"`
		}
		for {
			health = struct {
				Components []component `json:"components"`
			}{}
			if err := getJSON(ctx, v.Client, rawURL, "", "", &health); err == nil {
				for _, item := range health.Components {
					if item.Name == "core" && item.Status == "healthy" {
						return StatusOK, "core healthy"
					}
				}
			}
			if ctx.Err() != nil {
				return StatusFail, "core health check failed"
			}
			time.Sleep(3 * time.Second)
		}
	})
}

func (v Validator) OpenBao(ctx context.Context) CheckResult {
	return timed("OpenBao", func() (Status, string) {
		if v.Config.ValidateMockAll {
			return StatusSkipped, "ADMIN_NODE_VALIDATE_MOCK_ALL=true"
		}
		for range 60 {
			result := v.Runner.Run(ctx, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "openbao", "bao", "status", "-format=json")
			if result.Code == 0 || result.Code == 2 {
				var status struct {
					Initialized bool `json:"initialized"`
					Sealed      bool `json:"sealed"`
				}
				if err := json.Unmarshal([]byte(result.Stdout), &status); err == nil && status.Initialized && !status.Sealed {
					token := getenv("OPENBAO_TOKEN", "")
					if token != "" {
						put := v.Runner.Run(ctx, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN="+token, "openbao", "bao", "kv", "put", "secret/admin-node-sentinel", "value=ok")
						if put.Code != 0 {
							v.Runner.Run(ctx, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN="+token, "openbao", "bao", "secrets", "enable", "-path=secret", "kv-v2")
							put = v.Runner.Run(ctx, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN="+token, "openbao", "bao", "kv", "put", "secret/admin-node-sentinel", "value=ok")
							if put.Code != 0 {
								return StatusFail, "sentinel write failed: " + strings.TrimSpace(put.Stderr)
							}
						}
						get := v.Runner.Run(ctx, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN="+token, "openbao", "bao", "kv", "get", "-field=value", "secret/admin-node-sentinel")
						if get.Code != 0 || strings.TrimSpace(get.Stdout) != "ok" {
							return StatusFail, "sentinel read failed"
						}
					}
					return StatusOK, "initialized=true sealed=false"
				}
				if status.Initialized && status.Sealed {
					v.Runner.Run(ctx, filepath.Join(v.Config.RepoRoot, "scripts/openbao-unseal.sh"))
				}
			}
			time.Sleep(2 * time.Second)
		}
		return StatusFail, "not ready or still sealed"
	})
}

func (v Validator) Gitea(ctx context.Context) CheckResult {
	return timed("Gitea", func() (Status, string) {
		if v.Config.ValidateMockAll {
			return StatusSkipped, "ADMIN_NODE_VALIDATE_MOCK_ALL=true"
		}
		adminUser := getenv("GITEA_ADMIN_USER", "admin")
		adminPassword := getenv("GITEA_ADMIN_PASSWORD", "")
		if adminPassword == "" {
			adminPassword = readEnvFileValue(filepath.Join(v.Config.AdminRoot, "env/gitea.env"), "GITEA_ADMIN_PASSWORD")
		}
		if adminPassword == "" {
			return StatusFail, "GITEA_ADMIN_PASSWORD is required"
		}

		ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		versionURL := serviceURL(v.Config.GiteaDomain, "/api/v1/version")
		for {
			req, err := httpRequest(ctx, versionURL)
			if err == nil {
				resp, err := v.Client.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
						break
					}
				}
			}
			if ctx.Err() != nil {
				return StatusFail, "API version endpoint unreachable"
			}
			time.Sleep(3 * time.Second)
		}
		if err := v.ensureGiteaAdminAuth(ctx, adminUser, adminPassword); err != nil {
			return StatusFail, err.Error()
		}

		repo := getenv("GITEA_VALIDATION_REPO", "admin-node-validation")
		issueTitle := getenv("GITEA_VALIDATION_ISSUE_TITLE", "Backup restore sentinel")
		create := getenv("GITEA_VALIDATION_CREATE", "true") == "true"
		repoPath := fmt.Sprintf("/api/v1/repos/%s/%s", adminUser, repo)
		repoURL := serviceURL(v.Config.GiteaDomain, repoPath)
		var repoPayload map[string]any
		if err := getJSON(ctx, v.Client, repoURL, adminUser, adminPassword, &repoPayload); err != nil {
			if !create {
				return StatusFail, "validation repository not found"
			}
			createURL := serviceURL(v.Config.GiteaDomain, "/api/v1/user/repos")
			body := map[string]any{"name": repo, "private": true, "auto_init": true, "description": "Admin node backup/restore validation repository"}
			if err := postJSON(ctx, v.Client, createURL, adminUser, adminPassword, body, nil); err != nil {
				return StatusFail, "validation repository create failed: " + err.Error()
			}
		}

		issuesURL := serviceURL(v.Config.GiteaDomain, repoPath+"/issues?state=all&limit=100")
		var issues []struct {
			Title string `json:"title"`
		}
		if err := getJSON(ctx, v.Client, issuesURL, adminUser, adminPassword, &issues); err != nil {
			return StatusFail, "validation issues read failed: " + err.Error()
		}
		for _, issue := range issues {
			if issue.Title == issueTitle {
				return StatusOK, "validation repo and issue present"
			}
		}
		if !create {
			return StatusFail, "validation issue not found"
		}
		body := map[string]any{"title": issueTitle, "body": "Sentinel issue used to validate Gitea backup and restore."}
		if err := postJSON(ctx, v.Client, serviceURL(v.Config.GiteaDomain, repoPath+"/issues"), adminUser, adminPassword, body, nil); err != nil {
			return StatusFail, "validation issue create failed: " + err.Error()
		}
		return StatusOK, "validation repo and issue present"
	})
}

func (v Validator) ensureGiteaAdminAuth(ctx context.Context, adminUser string, adminPassword string) error {
	userURL := serviceURL(v.Config.GiteaDomain, "/api/v1/user")
	status, err := statusCode(ctx, v.Client, http.MethodGet, userURL, adminUser, adminPassword)
	if err != nil {
		return fmt.Errorf("Gitea admin API check failed: %w", err)
	}
	if status == http.StatusOK {
		return nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return fmt.Errorf("Gitea admin API check returned HTTP %d", status)
	}

	containers := v.Runner.Run(ctx, "docker", "ps", "--format", "{{.Names}}")
	if containers.Code != 0 || !strings.Contains(containers.Stdout, "gitea") {
		return fmt.Errorf("Gitea admin API auth failed and container is unavailable")
	}

	v.Runner.Run(ctx, "docker", "exec", "--user", "git", "gitea", "gitea", "admin", "user", "create",
		"--admin",
		"--must-change-password=false",
		"--username", adminUser,
		"--password", adminPassword,
		"--email", getenv("GITEA_ADMIN_EMAIL", "admin@example.com"),
		"--config", "/data/gitea/conf/app.ini",
	)
	change := v.Runner.Run(ctx, "docker", "exec", "--user", "git", "gitea", "gitea", "admin", "user", "change-password",
		"--username", adminUser,
		"--password", adminPassword,
		"--must-change-password=false",
		"--config", "/data/gitea/conf/app.ini",
	)
	if change.Code != 0 {
		return fmt.Errorf("Gitea admin password reset failed: %s", strings.TrimSpace(change.Stderr))
	}

	status, err = statusCode(ctx, v.Client, http.MethodGet, userURL, adminUser, adminPassword)
	if err != nil {
		return fmt.Errorf("Gitea admin API recheck failed: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("Gitea admin API auth still failed after CLI reset: HTTP %d", status)
	}
	return nil
}

func (v Validator) Traefik(ctx context.Context) CheckResult {
	return timed("Traefik", func() (Status, string) {
		if v.Config.ValidateMockAll {
			return StatusSkipped, "ADMIN_NODE_VALIDATE_MOCK_ALL=true"
		}
		user := getenv("TRAEFIK_DASHBOARD_USER", "")
		pass := getenv("TRAEFIK_DASHBOARD_PASS", "")
		if user == "" || pass == "" {
			if creds, err := os.ReadFile("/etc/admin-node/traefik-dashboard-creds"); err == nil {
				for _, line := range strings.Split(string(creds), "\n") {
					if strings.HasPrefix(line, "TRAEFIK_DASHBOARD_USER=") {
						user = strings.TrimPrefix(line, "TRAEFIK_DASHBOARD_USER=")
					}
					if strings.HasPrefix(line, "TRAEFIK_DASHBOARD_PASS=") {
						pass = strings.TrimPrefix(line, "TRAEFIK_DASHBOARD_PASS=")
					}
				}
			}
		}
		if user == "" || pass == "" {
			return StatusWarn, "dashboard credentials unavailable, route validation skipped"
		}
		var routers []map[string]any
		rawURL := serviceURL(v.Config.TraefikDomain, "/api/http/routers")
		if err := getJSON(ctx, v.Client, rawURL, user, pass, &routers); err != nil {
			return StatusFail, "dashboard API unreachable: " + err.Error()
		}
		encoded := fmt.Sprintf("%v", routers)
		for _, domain := range []string{v.Config.KeycloakDomain, v.Config.OpenBaoDomain, v.Config.HarborDomain, v.Config.GiteaDomain} {
			if !strings.Contains(encoded, strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")) {
				return StatusFail, "route not configured for " + domain
			}
		}
		return StatusOK, "routes configured"
	})
}

func (v Validator) DNS(_ context.Context) CheckResult {
	return timed("DNS", func() (Status, string) {
		if v.Config.ValidateMockAll {
			return StatusSkipped, "ADMIN_NODE_VALIDATE_MOCK_ALL=true"
		}
		if v.Config.CIMockPihole {
			return StatusSkipped, "CI_MOCK_PIHOLE=true"
		}
		for _, host := range []string{v.Config.HarborDomain, v.Config.OpenBaoDomain, v.Config.KeycloakDomain, v.Config.GiteaDomain, v.Config.TraefikDomain} {
			cleanHost := strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
			addrs, err := net.LookupHost(cleanHost)
			if err != nil || len(addrs) == 0 {
				return StatusFail, "cannot resolve " + cleanHost
			}
			matched := false
			for _, addr := range addrs {
				if addr == v.Config.AdminNodeLANIP {
					matched = true
					break
				}
			}
			if !matched {
				return StatusFail, fmt.Sprintf("%s does not resolve to %s", cleanHost, v.Config.AdminNodeLANIP)
			}
		}
		return StatusOK, "service domains resolve to admin node"
	})
}

func (v Validator) Tunnel(ctx context.Context) CheckResult {
	return timed("Tunnel", func() (Status, string) {
		if v.Config.ValidateMockAll {
			return StatusSkipped, "ADMIN_NODE_VALIDATE_MOCK_ALL=true"
		}
		if v.Config.CIMockCloudflareTunnel {
			result := v.Runner.Run(ctx, "docker", "ps", "-a", "--format", "{{.Names}}")
			if strings.Contains(result.Stdout, "cloudflared") {
				return StatusOK, "cloudflared container exists (CI mock mode)"
			}
			return StatusOK, "cloudflared container not found but mocking enabled"
		}
		all := v.Runner.Run(ctx, "docker", "ps", "-a", "--format", "{{.Names}}")
		if all.Code != 0 {
			return StatusFail, "docker ps failed: " + strings.TrimSpace(all.Stderr)
		}
		if !strings.Contains(all.Stdout, "cloudflared") {
			return StatusFail, "cloudflared container does not exist"
		}
		running := v.Runner.Run(ctx, "docker", "ps", "--format", "{{.Names}}")
		if running.Code != 0 {
			return StatusFail, "docker ps failed: " + strings.TrimSpace(running.Stderr)
		}
		if !strings.Contains(running.Stdout, "cloudflared") {
			return StatusFail, "cloudflared container exists but is not running"
		}
		if !v.Config.SkipPublicURLValidation && !v.Config.CISkipPublicURLValidation {
			for _, domain := range []string{v.Config.KeycloakDomain, v.Config.OpenBaoDomain, v.Config.HarborDomain, v.Config.GiteaDomain, v.Config.TraefikDomain} {
				req, err := httpRequest(ctx, serviceURL(domain, "/"))
				if err != nil {
					return StatusFail, err.Error()
				}
				resp, err := v.Client.Do(req)
				if err != nil {
					return StatusFail, "public URL validation failed for " + domain + ": " + err.Error()
				}
				resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode > 399 {
					return StatusFail, fmt.Sprintf("public URL validation failed for %s: HTTP %d", domain, resp.StatusCode)
				}
			}
		}
		return StatusOK, "cloudflared container running"
	})
}

func httpRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func readEnvFileValue(path, key string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return ""
}
