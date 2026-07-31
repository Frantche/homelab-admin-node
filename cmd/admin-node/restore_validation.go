package main

import (
	"context"
	"strings"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/runner"
	"github.com/Frantche/homelab-admin-node/internal/validate"
)

func restoreValidation(ctx context.Context, run runner.Runner) []validate.CheckResult {
	return []validate.CheckResult{
		restoreDockerHealth(ctx, run, "OpenBao", "openbao"),
		restoreDockerHealth(ctx, run, "Keycloak", "keycloak"),
		restoreDockerHealth(ctx, run, "Harbor", "harbor-core"),
		restoreDockerHealth(ctx, run, "Gitea", "gitea"),
		restoreDockerHealth(ctx, run, "Traefik", "traefik"),
		restoreCommandCheck(ctx, run, "OpenBao", 90*time.Second, "docker", "exec", "-e", "BAO_ADDR=https://127.0.0.1:8200", "-e", "BAO_CACERT=/openbao/tls/ca.pem", "openbao", "sh", "-c", "bao status -format=json | grep '\"sealed\": false' >/dev/null"),
		restoreCommandCheck(ctx, run, "Keycloak", 120*time.Second, "docker", "exec", "keycloak", "bash", "-lc", "timeout 3 bash -c 'exec 3<>/dev/tcp/127.0.0.1/9000; printf \"GET /health/ready HTTP/1.1\\r\\nHost: localhost\\r\\nConnection: close\\r\\n\\r\\n\" >&3; head -1 <&3 | grep -q \"200\"'"),
		restoreCommandCheck(ctx, run, "Harbor", 120*time.Second, "docker", "exec", "harbor-core", "curl", "-fsS", "http://127.0.0.1:8080/api/v2.0/health"),
		restoreCommandCheck(ctx, run, "Gitea", 120*time.Second, "docker", "exec", "gitea", "curl", "-fsS", "http://127.0.0.1:3000/api/v1/version"),
		restoreCommandCheck(ctx, run, "Gitea data", 120*time.Second, "docker", "exec", "gitea-db", "psql", "-U", "gitea", "-d", "gitea", "-Atqc", `SELECT 1 FROM "user" LIMIT 1`),
		restoreCommandCheck(ctx, run, "Traefik", 90*time.Second, "docker", "exec", "traefik", "traefik", "healthcheck", "--ping"),
	}
}

func restoreDockerHealth(ctx context.Context, run runner.Runner, name, container string) validate.CheckResult {
	return restoreWait(ctx, name, 120*time.Second, func(ctx context.Context) (validate.Status, string, bool) {
		result := run.Run(ctx, "docker", "inspect", "-f", "{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", container)
		if result.Code != 0 {
			return validate.StatusFail, container + " is unavailable", false
		}
		fields := strings.Fields(result.Stdout)
		if len(fields) < 2 || fields[0] != "true" {
			return validate.StatusFail, container + " is not running", false
		}
		if fields[1] == "healthy" || fields[1] == "none" {
			return validate.StatusOK, container + " running " + fields[1], true
		}
		return validate.StatusFail, container + " health is " + fields[1], false
	})
}

func restoreCommandCheck(ctx context.Context, run runner.Runner, name string, timeout time.Duration, command string, args ...string) validate.CheckResult {
	return restoreWait(ctx, name, timeout, func(ctx context.Context) (validate.Status, string, bool) {
		result := run.Run(ctx, command, args...)
		if result.Code == 0 {
			return validate.StatusOK, "internal endpoint reachable", true
		}
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = strings.TrimSpace(result.Stdout)
		}
		if message == "" {
			message = "internal endpoint unavailable"
		}
		return validate.StatusFail, message, false
	})
}

func restoreWait(ctx context.Context, name string, timeout time.Duration, check func(context.Context) (validate.Status, string, bool)) validate.CheckResult {
	start := time.Now()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var status validate.Status
	var message string
	var ok bool
	for {
		status, message, ok = check(waitCtx)
		if ok || waitCtx.Err() != nil {
			break
		}
		select {
		case <-waitCtx.Done():
		case <-time.After(3 * time.Second):
		}
	}
	return validate.CheckResult{Name: name, Status: status, Message: message, Duration: time.Since(start)}
}
