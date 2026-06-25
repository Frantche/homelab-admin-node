package config

import "testing"

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("ADMIN_NODE_REPO_ROOT", "")
	t.Setenv("ADMIN_NODE_ROOT", "")
	t.Setenv("CI_MODE", "")

	cfg := FromEnv()

	if cfg.RepoRoot != DefaultRepoRoot {
		t.Fatalf("RepoRoot = %q, want %q", cfg.RepoRoot, DefaultRepoRoot)
	}
	if cfg.AdminRoot != DefaultAdminRoot {
		t.Fatalf("AdminRoot = %q, want %q", cfg.AdminRoot, DefaultAdminRoot)
	}
	if cfg.AdminNodeLANIP != DefaultAdminNodeLANIP {
		t.Fatalf("AdminNodeLANIP = %q, want %q", cfg.AdminNodeLANIP, DefaultAdminNodeLANIP)
	}
	if cfg.CIMode {
		t.Fatal("CIMode = true, want false")
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("ADMIN_NODE_REPO_ROOT", "/tmp/repo")
	t.Setenv("ADMIN_NODE_ROOT", "/tmp/admin")
	t.Setenv("KEYCLOAK_DOMAIN", "keycloak.test")
	t.Setenv("ADMIN_NODE_LAN_IP", "10.0.0.10")
	t.Setenv("CI_MODE", "true")
	t.Setenv("CI_MOCK_PIHOLE", "true")
	t.Setenv("ADMIN_NODE_VALIDATE_MOCK_ALL", "true")

	cfg := FromEnv()

	if cfg.RepoRoot != "/tmp/repo" {
		t.Fatalf("RepoRoot = %q", cfg.RepoRoot)
	}
	if cfg.AdminRoot != "/tmp/admin" {
		t.Fatalf("AdminRoot = %q", cfg.AdminRoot)
	}
	if cfg.KeycloakDomain != "keycloak.test" {
		t.Fatalf("KeycloakDomain = %q", cfg.KeycloakDomain)
	}
	if cfg.AdminNodeLANIP != "10.0.0.10" {
		t.Fatalf("AdminNodeLANIP = %q", cfg.AdminNodeLANIP)
	}
	if !cfg.CIMode {
		t.Fatal("CIMode = false, want true")
	}
	if !cfg.CIMockPihole {
		t.Fatal("CIMockPihole = false, want true")
	}
	if !cfg.ValidateMockAll {
		t.Fatal("ValidateMockAll = false, want true")
	}
}
