package converge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitRootForPathFindsInventoryRepository(t *testing.T) {
	ctx := context.Background()
	repo := initGitRepo(t)
	inventory := filepath.Join(repo, "hosts", "inventory.ini")
	if err := os.MkdirAll(filepath.Dir(inventory), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inventory, []byte("localhost ansible_connection=local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := gitRootForPath(ctx, inventory)
	if err != nil {
		t.Fatalf("gitRootForPath returned error: %v", err)
	}
	if root != repo {
		t.Fatalf("git root = %q, want %q", root, repo)
	}
}

func TestUpdateGitRepositoryPullsWhenUpstreamHasNewCommit(t *testing.T) {
	ctx := context.Background()
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "--bare", "--initial-branch=main", origin)

	source := filepath.Join(t.TempDir(), "source")
	git(t, "", "clone", origin, source)
	configureGitUser(t, source)
	writeAndCommit(t, source, "hosts/inventory.ini", "localhost\n", "initial inventory")
	git(t, source, "push", "-u", "origin", "main")

	local := filepath.Join(t.TempDir(), "local")
	git(t, "", "clone", origin, local)
	writeAndCommit(t, source, "hosts/group_vars/all.yml", "admin_mode: normal\n", "update inventory config")
	git(t, source, "push")

	before := gitOutput(t, local, "rev-parse", "HEAD")
	if err := updateGitRepository(ctx, local, "inventory repo"); err != nil {
		t.Fatalf("updateGitRepository returned error: %v", err)
	}
	after := gitOutput(t, local, "rev-parse", "HEAD")
	if after == before {
		t.Fatal("local repository did not move after update")
	}
	if after != gitOutput(t, local, "rev-parse", "origin/main") {
		t.Fatalf("local HEAD = %s, want origin/main", after)
	}
	if _, err := os.Stat(filepath.Join(local, "hosts/group_vars/all.yml")); err != nil {
		t.Fatalf("new inventory config file was not pulled: %v", err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, "", "init", "--initial-branch=main", repo)
	configureGitUser(t, repo)
	return repo
}

func configureGitUser(t *testing.T, repo string) {
	t.Helper()
	git(t, repo, "config", "user.email", "ci@example.test")
	git(t, repo, "config", "user.name", "CI")
}

func writeAndCommit(t *testing.T, repo, name, content, message string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", name)
	git(t, repo, "commit", "-m", message)
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
