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
	"github.com/Frantche/homelab-admin-node/internal/openbao"
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
	if !v.Config.PiholeDisabled {
		results = append(results, v.DNS(ctx))
	}
	if !v.Config.CloudflareDisabled {
		results = append(results, v.Tunnel(ctx))
	}
	return results
}

func (v Validator) Observability(ctx context.Context) CheckResult {
	return timed("Observability", func() (Status, string) {
		if v.Config.ValidateMockAll {
			return StatusSkipped, "ADMIN_NODE_VALIDATE_MOCK_ALL=true"
		}

		if result := v.Runner.Run(ctx, "docker", "inspect", "-f", "{{.State.Running}}", "otel-collector"); result.Code != 0 {
			return StatusFail, "otel-collector container is unavailable"
		} else if strings.TrimSpace(result.Stdout) != "true" {
			return StatusFail, "otel-collector container is not running"
		}

		version := v.Runner.Run(ctx, "docker", "exec", "otel-collector", "/otelcol-contrib", "--version")
		if version.Code != 0 {
			return StatusFail, "otel-collector binary is unavailable"
		}

		mockDir := getenv("CI_OTEL_MOCK_STATE_DIR", "")
		if mockDir == "" {
			return StatusOK, "collector running"
		}

		sentinelName := fmt.Sprintf("admin-node-otel-log-sentinel-%d", time.Now().UnixNano())
		sentinel := v.Runner.Run(ctx, "docker", "run", "-d", "--name", sentinelName, "--network", "admin-net", "alpine:3.20", "sh", "-c", "echo admin-node-otel-log-sentinel; sleep 30")
		if sentinel.Code != 0 {
			return StatusFail, "failed to create log sentinel container: " + strings.TrimSpace(sentinel.Stderr)
		}
		defer v.Runner.Run(context.Background(), "docker", "rm", "-f", sentinelName)

		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		for {
			missingMetrics, missingLogs := missingObservabilityMockContent(mockDir)
			if len(missingMetrics) == 0 && len(missingLogs) == 0 {
				return StatusOK, "collector exported expected service metrics and sentinel logs to CI OTLP mock"
			}
			if ctx.Err() != nil {
				missing := []string{}
				if len(missingMetrics) > 0 {
					missing = append(missing, "metrics content: "+strings.Join(missingMetrics, ", "))
				}
				if len(missingLogs) > 0 {
					missing = append(missing, "logs content: "+strings.Join(missingLogs, ", "))
				}
				return StatusFail, "CI OTLP mock missing " + strings.Join(missing, " and ")
			}
			time.Sleep(3 * time.Second)
		}
	})
}

func missingObservabilityMockContent(mockDir string) ([]string, []string) {
	metricsContent := fileContent(filepath.Join(mockDir, "metrics.received"))
	logsContent := fileContent(filepath.Join(mockDir, "logs.received"))

	metricMarkers := []struct {
		name    string
		markers []string
	}{
		{name: "gitea", markers: []string{`"key":"service.name","value":{"stringValue":"gitea"}`}},
		{name: "harbor-core", markers: []string{`"key":"service.name","value":{"stringValue":"harbor-core"}`}},
		{name: "harbor-exporter", markers: []string{`"key":"service.name","value":{"stringValue":"harbor-exporter"}`}},
		{name: "openbao", markers: []string{`"key":"service.name","value":{"stringValue":"openbao"}`}},
		{name: "traefik", markers: []string{`"key":"service.name","value":{"stringValue":"traefik"}`}},
	}
	missingMetrics := []string{}
	for _, expected := range metricMarkers {
		if !containsAny(metricsContent, expected.markers) {
			missingMetrics = append(missingMetrics, expected.name)
		}
	}

	missingLogs := []string{}
	if !strings.Contains(logsContent, "admin-node-otel-log-sentinel") {
		missingLogs = append(missingLogs, "admin-node-otel-log-sentinel")
	}
	return missingMetrics, missingLogs
}

func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
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
						adminPassword := getenv("HARBOR_ADMIN_PASSWORD", "")
						if adminPassword == "" {
							adminPassword = readEnvFileValue(filepath.Join(v.Config.AdminRoot, "env/harbor.env"), "HARBOR_ADMIN_PASSWORD")
						}
						if adminPassword == "" {
							return StatusOK, "core healthy"
						}
						adminUser := getenv("HARBOR_ADMIN_USER", "admin")
						var projects []map[string]any
						projectsURL := serviceURL(v.Config.HarborDomain, "/api/v2.0/projects?page=1&page_size=1")
						if err := getJSON(ctx, v.Client, projectsURL, adminUser, adminPassword, &projects); err != nil {
							return StatusFail, "admin API check failed: " + err.Error()
						}
						return StatusOK, "core healthy and admin API reachable"
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
					_ = openbao.Unseal(ctx, openbao.Options{})
				}
			}
			time.Sleep(2 * time.Second)
		}
		return StatusFail, "not ready or still sealed"
	})
}

func (v Validator) Hardening(ctx context.Context) CheckResult {
	return timed("Hardening", func() (Status, string) {
		if result := v.Runner.Run(ctx, "sshd", "-T"); result.Code == 0 {
			expectedSSH := []string{
				"permitrootlogin no",
				"passwordauthentication no",
				"kbdinteractiveauthentication no",
				"pubkeyauthentication yes",
				"allowtcpforwarding no",
				"allowagentforwarding no",
				"clientalivecountmax 2",
				"loglevel VERBOSE",
				"maxauthtries 3",
				"maxsessions 2",
				"tcpkeepalive no",
			}
			for _, expected := range expectedSSH {
				if !containsLine(result.Stdout, expected) {
					return StatusFail, "sshd option mismatch: expected " + expected
				}
			}
		}
		expectedSysctls := map[string]string{
			"dev.tty.ldisc_autoload":             "0",
			"fs.protected_fifos":                 "2",
			"fs.protected_regular":               "2",
			"fs.suid_dumpable":                   "0",
			"kernel.sysrq":                       "0",
			"kernel.kptr_restrict":               "2",
			"kernel.dmesg_restrict":              "1",
			"kernel.unprivileged_bpf_disabled":   "1",
			"net.core.bpf_jit_harden":            "2",
			"net.ipv4.conf.all.log_martians":     "1",
			"net.ipv4.conf.default.log_martians": "1",
		}
		for key, expected := range expectedSysctls {
			result := v.Runner.Run(ctx, "sysctl", "-n", key)
			if result.Code != 0 {
				return StatusFail, "sysctl command failed for " + key
			}
			if strings.TrimSpace(result.Stdout) != expected {
				return StatusFail, fmt.Sprintf("sysctl mismatch: %s expected %s", key, expected)
			}
		}
		if result := v.Runner.Run(ctx, "systemctl", "is-active", "--quiet", "systemd-journald"); result.Code != 0 {
			return StatusFail, "systemd-journald is not active"
		}
		for _, path := range []string{
			"/etc/security/limits.d/90-admin-core-dumps.conf",
			"/etc/modprobe.d/90-admin-hardening.conf",
			"/etc/issue.net",
		} {
			if _, err := os.Stat(path); err != nil {
				return StatusFail, path + " is missing"
			}
		}
		if result := v.Runner.Run(ctx, "nft", "list", "table", "inet", "admin_filter"); result.Code != 0 {
			return StatusFail, "nftables admin_filter table is unavailable"
		} else if !strings.Contains(result.Stdout, "policy drop") || !strings.Contains(result.Stdout, "tcp dport 22 accept") || !strings.Contains(result.Stdout, "tcp dport 443 accept") {
			return StatusFail, "nftables policy is incomplete"
		}
		loginDefs, err := os.ReadFile("/etc/login.defs")
		if err != nil {
			return StatusFail, "/etc/login.defs is unreadable"
		}
		for _, expected := range []string{"UMASK 027", "PASS_MIN_DAYS 1", "PASS_MAX_DAYS 365"} {
			if !containsFieldsLine(string(loginDefs), expected) {
				return StatusFail, "/etc/login.defs mismatch: expected " + expected
			}
		}
		modprobe, err := os.ReadFile("/etc/modprobe.d/90-admin-hardening.conf")
		if err != nil {
			return StatusFail, "modprobe hardening drop-in is unreadable"
		}
		for _, module := range []string{"usb-storage", "firewire-ohci", "dccp", "sctp", "rds", "tipc"} {
			if !containsLine(string(modprobe), "install "+module+" /bin/false") {
				return StatusFail, "module is not disabled: " + module
			}
		}
		for _, unit := range []string{"auditd", "fail2ban"} {
			if list := v.Runner.Run(ctx, "systemctl", "list-unit-files", unit+".service"); list.Code == 0 {
				active := v.Runner.Run(ctx, "systemctl", "is-active", "--quiet", unit)
				if active.Code != 0 {
					return StatusFail, unit + " is not active"
				}
			}
		}
		if _, err := os.Stat("/var/log/journal"); err != nil {
			return StatusFail, "persistent journal directory is missing"
		}
		if _, err := os.Stat("/etc/sops/age/keys.txt"); err == nil {
			if info, err := os.Stat("/etc/sops/age/keys.txt"); err == nil && info.Mode().Perm() != 0o400 {
				return StatusFail, "/etc/sops/age/keys.txt permissions are not 0400"
			}
		}
		return StatusOK, "system hardening checks passed"
	})
}

func containsLine(text string, expected string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}

func containsFieldsLine(text string, expected string) bool {
	expectedFields := strings.Fields(expected)
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != len(expectedFields) {
			continue
		}
		matched := true
		for i := range fields {
			if fields[i] != expectedFields[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
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
		if err := getJSON(ctx, v.Client, repoURL, adminUser, adminPassword, &giteaRepo{}); err != nil {
			if !create {
				return StatusFail, "validation repository not found"
			}
			createURL := serviceURL(v.Config.GiteaDomain, "/api/v1/user/repos")
			body := map[string]any{"name": repo, "private": true, "auto_init": true, "description": "Admin node backup/restore validation repository"}
			if err := postJSON(ctx, v.Client, createURL, adminUser, adminPassword, body, nil); err != nil {
				return StatusFail, "validation repository create failed: " + err.Error()
			}
		}
		repoPayload, err := readGiteaRepo(ctx, v.Client, repoURL, adminUser, adminPassword)
		if err != nil {
			return StatusFail, "validation repository read failed after ensure: " + err.Error()
		}
		if repoPayload.Name != repo {
			return StatusFail, "validation repository name mismatch"
		}
		if repoPayload.Owner.Login != adminUser {
			return StatusFail, "validation repository owner mismatch"
		}
		if !repoPayload.Private {
			return StatusFail, "validation repository is not private"
		}

		issuesURL := serviceURL(v.Config.GiteaDomain, repoPath+"/issues?state=all&limit=100")
		issues, err := readGiteaIssues(ctx, v.Client, issuesURL, adminUser, adminPassword)
		if err != nil {
			return StatusFail, "validation issues read failed: " + err.Error()
		}
		if !hasGiteaIssue(issues, issueTitle) {
			if !create {
				return StatusFail, "validation issue not found"
			}
			body := map[string]any{"title": issueTitle, "body": "Sentinel issue used to validate Gitea backup and restore."}
			if err := postJSON(ctx, v.Client, serviceURL(v.Config.GiteaDomain, repoPath+"/issues"), adminUser, adminPassword, body, nil); err != nil {
				return StatusFail, "validation issue create failed: " + err.Error()
			}
		}
		issues, err = readGiteaIssues(ctx, v.Client, issuesURL, adminUser, adminPassword)
		if err != nil {
			return StatusFail, "validation issues reread failed: " + err.Error()
		}
		if !hasGiteaIssue(issues, issueTitle) {
			return StatusFail, "validation issue not found after create/read"
		}
		return StatusOK, "validation repo and issue present"
	})
}

type giteaRepo struct {
	Name    string `json:"name"`
	Private bool   `json:"private"`
	Owner   struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type giteaIssue struct {
	Title string `json:"title"`
}

func readGiteaRepo(ctx context.Context, client HTTPDoer, rawURL string, adminUser string, adminPassword string) (giteaRepo, error) {
	var repo giteaRepo
	err := getJSON(ctx, client, rawURL, adminUser, adminPassword, &repo)
	return repo, err
}

func readGiteaIssues(ctx context.Context, client HTTPDoer, rawURL string, adminUser string, adminPassword string) ([]giteaIssue, error) {
	var issues []giteaIssue
	err := getJSON(ctx, client, rawURL, adminUser, adminPassword, &issues)
	return issues, err
}

func hasGiteaIssue(issues []giteaIssue, title string) bool {
	for _, issue := range issues {
		if issue.Title == title {
			return true
		}
	}
	return false
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
		if v.Config.PiholeDisabled {
			return StatusSkipped, "PIHOLE_ENABLED=false"
		}
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
		if v.Config.CloudflareDisabled {
			return StatusSkipped, "CLOUDFLARE_ENABLED=false"
		}
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
