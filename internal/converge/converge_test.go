package converge

import (
	"context"
	"errors"
	"fmt"
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

func TestUpdateAdminRepositoryKeepsImmutableCommitPin(t *testing.T) {
	ctx := context.Background()
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "--bare", "--initial-branch=main", origin)
	source := filepath.Join(t.TempDir(), "source")
	git(t, "", "clone", origin, source)
	configureGitUser(t, source)
	writeAndCommit(t, source, "release/config-schema-version", "1\n", "initial")
	git(t, source, "push", "-u", "origin", "main")
	pinned := gitOutput(t, source, "rev-parse", "HEAD")

	local := filepath.Join(t.TempDir(), "local")
	git(t, "", "clone", origin, local)
	writeAndCommit(t, source, "README.md", "newer\n", "newer")
	git(t, source, "push")
	refFile := filepath.Join(t.TempDir(), "release-ref")
	if err := os.WriteFile(refFile, []byte(pinned+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := updateAdminRepository(ctx, Options{RepoDir: local, ReleaseRefFile: refFile}, true); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, local, "rev-parse", "HEAD"); got != pinned {
		t.Fatalf("HEAD = %s, want immutable pin %s", got, pinned)
	}
	if branch := gitOutput(t, local, "branch", "--show-current"); branch != "" {
		t.Fatalf("pinned checkout remains on moving branch %q", branch)
	}
}

func TestUpdateAdminRepositoryRefusesMissingReleasePin(t *testing.T) {
	err := updateAdminRepository(context.Background(), Options{
		RepoDir:        initGitRepo(t),
		ReleaseRefFile: filepath.Join(t.TempDir(), "missing-release-ref"),
	}, true)
	if err == nil || !strings.Contains(err.Error(), "release pin") {
		t.Fatalf("missing release pin error = %v", err)
	}
}

func TestUpdateAdminRepositorySkipPullStillEnforcesPin(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "release/config-schema-version", "1\n", "initial")
	pinned := gitOutput(t, repo, "rev-parse", "HEAD")
	refFile := filepath.Join(t.TempDir(), "release-ref")
	if err := os.WriteFile(refFile, []byte(pinned+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := updateAdminRepository(context.Background(), Options{RepoDir: repo, ReleaseRefFile: refFile}, false)
	if err == nil || !strings.Contains(err.Error(), "not detached at release pin") {
		t.Fatalf("skip-pull pin error = %v", err)
	}

	git(t, repo, "checkout", "--detach", pinned)
	if err := updateAdminRepository(context.Background(), Options{RepoDir: repo, ReleaseRefFile: refFile}, false); err != nil {
		t.Fatalf("aligned offline checkout rejected: %v", err)
	}
}

func TestUpdateAdminRepositoryRefusesDirtyPinnedCheckout(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "tracked", path: "release/config-schema-version"},
		{name: "untracked", path: "local-override.yml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := initGitRepo(t)
			writeAndCommit(t, repo, "release/config-schema-version", "1\n", "initial")
			pinned := gitOutput(t, repo, "rev-parse", "HEAD")
			git(t, repo, "checkout", "--detach", pinned)
			if err := os.WriteFile(filepath.Join(repo, test.path), []byte("modified\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			refFile := filepath.Join(t.TempDir(), "release-ref")
			if err := os.WriteFile(refFile, []byte(pinned+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			err := updateAdminRepository(context.Background(), Options{RepoDir: repo, ReleaseRefFile: refFile}, false)
			if err == nil || !strings.Contains(err.Error(), "local changes") {
				t.Fatalf("dirty checkout error = %v", err)
			}
		})
	}
}

func TestUpdateAdminRepositoryMainDoesNotDiscardLocalCommits(t *testing.T) {
	ctx := context.Background()
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "--bare", "--initial-branch=main", origin)
	source := filepath.Join(t.TempDir(), "source")
	git(t, "", "clone", origin, source)
	configureGitUser(t, source)
	writeAndCommit(t, source, "release/config-schema-version", "1\n", "initial")
	git(t, source, "push", "-u", "origin", "main")

	local := filepath.Join(t.TempDir(), "local")
	git(t, "", "clone", origin, local)
	configureGitUser(t, local)
	writeAndCommit(t, local, "LOCAL.md", "keep me\n", "local development")
	localCommit := gitOutput(t, local, "rev-parse", "HEAD")
	refFile := filepath.Join(t.TempDir(), "release-ref")
	if err := os.WriteFile(refFile, []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := updateAdminRepository(ctx, Options{RepoDir: local, ReleaseRefFile: refFile}, true); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, local, "rev-parse", "HEAD"); got != localCommit {
		t.Fatalf("main update discarded local commit: HEAD=%s want=%s", got, localCommit)
	}
}

func TestPersistInstalledState(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "release/config-schema-version", "3\n", "schema")
	stateDir := t.TempDir()
	opts := Options{
		RepoDir:      repo,
		RevisionFile: filepath.Join(stateDir, "git-ref"),
		SchemaSource: filepath.Join(repo, "release/config-schema-version"),
		SchemaFile:   filepath.Join(stateDir, "schema"),
	}
	if err := persistInstalledState(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(readFile(t, opts.RevisionFile)); got != gitOutput(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("recorded revision = %s", got)
	}
	if got := strings.TrimSpace(readFile(t, opts.SchemaFile)); got != "3" {
		t.Fatalf("recorded schema = %q", got)
	}
}

func TestPersistInstalledStateDoesNotRecordRevisionForInvalidSchema(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "release/config-schema-version", "\n", "empty schema")
	stateDir := t.TempDir()
	opts := Options{
		RepoDir:      repo,
		RevisionFile: filepath.Join(stateDir, "git-ref"),
		SchemaSource: filepath.Join(repo, "release/config-schema-version"),
		SchemaFile:   filepath.Join(stateDir, "schema"),
	}
	if err := persistInstalledState(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("invalid schema error = %v", err)
	}
	if _, err := os.Stat(opts.RevisionFile); !os.IsNotExist(err) {
		t.Fatalf("revision marker should not exist after invalid schema: %v", err)
	}
}

func TestRebuildAdminNodeRequestsRestartOnlyWhenBinaryChanges(t *testing.T) {
	for _, test := range []struct {
		name    string
		changed bool
	}{
		{name: "unchanged", changed: false},
		{name: "rebuilt", changed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			script := filepath.Join(dir, "build-admin-node.sh")
			content := "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'changed=" + fmt.Sprintf("%t", test.changed) + "\\n'\n"
			if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
			changed, err := rebuildAdminNode(context.Background(), Options{BuildScript: script, BinaryPath: filepath.Join(dir, "admin-node")})
			if err != nil {
				t.Fatal(err)
			}
			if changed != test.changed {
				t.Fatalf("changed = %t, want %t", changed, test.changed)
			}
		})
	}
}

func TestRunRestartsTargetBinaryBeforeAnsibleAfterReleaseChange(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "release/config-schema-version", "1\n", "target release")
	target := gitOutput(t, repo, "rev-parse", "HEAD")
	writeAndCommit(t, repo, "README.md", "current release\n", "current release")
	stateDir := t.TempDir()
	refFile := filepath.Join(stateDir, "release-ref")
	if err := os.WriteFile(refFile, []byte(target+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildScript := filepath.Join(t.TempDir(), "build-admin-node.sh")
	if err := os.WriteFile(buildScript, []byte("#!/usr/bin/env bash\nset -euo pipefail\nprintf 'changed=true\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		RepoDir:        repo,
		InventoryPath:  filepath.Join(t.TempDir(), "inventory-must-not-be-read"),
		PlaybookPath:   filepath.Join(t.TempDir(), "playbook-must-not-be-read"),
		LockFile:       filepath.Join(t.TempDir(), "converge.lock"),
		ReleaseRefFile: refFile,
		SchemaSource:   filepath.Join(repo, "release/config-schema-version"),
		BuildScript:    buildScript,
		BinaryPath:     filepath.Join(repo, "bin/admin-node"),
	}
	err := Run(context.Background(), opts)
	var restart *RestartRequiredError
	if !errors.As(err, &restart) {
		t.Fatalf("release transition error = %v, want RestartRequiredError", err)
	}
	if got := gitOutput(t, repo, "rev-parse", "HEAD"); got != target {
		t.Fatalf("HEAD = %s, want target %s", got, target)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
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
