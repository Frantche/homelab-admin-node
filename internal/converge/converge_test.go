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

func TestApproveAcceptsSignedReachableCommit(t *testing.T) {
	ctx := context.Background()
	gnupgHome := configureSigningKey(t)
	origin, source := signedOrigin(t)
	_ = origin
	writeAndCommit(t, source, "ansible/site.yml", "---\n", "signed update")
	git(t, source, "push")

	approval := filepath.Join(t.TempDir(), "approved")
	revision, err := Approve(ctx, source, "HEAD", approval, "origin/main", gnupgHome)
	if err != nil {
		t.Fatalf("Approve returned error: %v", err)
	}
	if got := strings.TrimSpace(readFile(t, approval)); got != revision {
		t.Fatalf("approval = %s, want %s", got, revision)
	}
}

func TestApproveRejectsUnsignedCommit(t *testing.T) {
	ctx := context.Background()
	gnupgHome := configureSigningKey(t)
	_, source := signedOrigin(t)
	git(t, source, "config", "commit.gpgsign", "false")
	writeAndCommit(t, source, "unsigned.txt", "unsafe\n", "unsigned update")
	git(t, source, "push")

	_, err := Approve(ctx, source, "HEAD", filepath.Join(t.TempDir(), "approved"), "origin/main", gnupgHome)
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %v, want signature rejection", err)
	}
}

func TestTrustedConvergenceRejectsTrackedLocalChanges(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "ansible/site.yml", "---\n", "initial")
	if err := os.WriteFile(filepath.Join(repo, "ansible/site.yml"), []byte("---\n# local change\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := prepareApprovedRepository(
		context.Background(),
		repo,
		"admin repo",
		filepath.Join(t.TempDir(), "approval"),
		t.TempDir(),
		"origin/main",
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "tracked local changes") {
		t.Fatalf("error = %v, want tracked change rejection", err)
	}
}

func TestRunRestoresPreviousRevisionAfterFailedUpdate(t *testing.T) {
	ctx := context.Background()
	gnupgHome := configureSigningKey(t)
	origin, source := signedOrigin(t)
	writeAndCommit(t, source, "ansible/site.yml", "---\n", "initial playbook")
	git(t, source, "push")

	runtime := filepath.Join(t.TempDir(), "runtime")
	git(t, "", "clone", origin, runtime)
	previous := gitOutput(t, runtime, "rev-parse", "HEAD")

	writeAndCommit(t, source, "release.txt", "candidate\n", "candidate release")
	git(t, source, "push")
	candidate := gitOutput(t, source, "rev-parse", "HEAD")
	approval := filepath.Join(t.TempDir(), "approved")
	if err := os.WriteFile(approval, []byte(candidate+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	fakeAnsible := filepath.Join(binDir, "ansible-playbook")
	script := "#!/usr/bin/env bash\nset -euo pipefail\ncurrent=\"$(git -C \"$TEST_RUNTIME\" rev-parse HEAD)\"\n[[ \"$current\" != \"$TEST_BAD_REVISION\" ]]\n"
	if err := os.WriteFile(fakeAnsible, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	t.Setenv("TEST_RUNTIME", runtime)
	t.Setenv("TEST_BAD_REVISION", candidate)

	err := Run(ctx, Options{
		RepoDir:          runtime,
		InventoryPath:    filepath.Join(runtime, "ansible/site.yml"),
		PlaybookPath:     filepath.Join(runtime, "ansible/site.yml"),
		LockFile:         filepath.Join(t.TempDir(), "converge.lock"),
		RequireApproval:  true,
		ApprovalFile:     approval,
		TrustedGNUPGHome: gnupgHome,
		Upstream:         "origin/main",
	})
	if err == nil || !strings.Contains(err.Error(), "previous approved release was restored") {
		t.Fatalf("error = %v, want successful rollback report", err)
	}
	if got := gitOutput(t, runtime, "rev-parse", "HEAD"); got != previous {
		t.Fatalf("runtime HEAD = %s, want rollback to %s", got, previous)
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

func configureSigningKey(t *testing.T) string {
	t.Helper()
	gnupgHome := t.TempDir()
	if err := os.Chmod(gnupgHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", gnupgHome)
	cmd := exec.Command("gpg", "--batch", "--passphrase", "", "--quick-gen-key", "CI Signer <ci@example.test>", "rsa2048", "sign", "0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate signing key: %v\n%s", err, output)
	}
	output := commandOutputForTest(t, "", "gpg", "--batch", "--with-colons", "--list-secret-keys")
	var fingerprint string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" {
			fingerprint = fields[9]
			break
		}
	}
	if fingerprint == "" {
		t.Fatal("generated signing key has no fingerprint")
	}
	t.Setenv("TEST_SIGNING_KEY", fingerprint)
	return gnupgHome
}

func signedOrigin(t *testing.T) (string, string) {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "--bare", "--initial-branch=main", origin)
	source := filepath.Join(t.TempDir(), "source")
	git(t, "", "clone", origin, source)
	configureGitUser(t, source)
	git(t, source, "config", "user.signingkey", os.Getenv("TEST_SIGNING_KEY"))
	git(t, source, "config", "commit.gpgsign", "true")
	writeAndCommit(t, source, "README.md", "trusted\n", "initial signed commit")
	git(t, source, "push", "-u", "origin", "main")
	return origin, source
}

func commandOutputForTest(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v", name, args, err)
	}
	return string(out)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
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
