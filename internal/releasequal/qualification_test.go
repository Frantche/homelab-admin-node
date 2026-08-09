package releasequal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVerifyProductionBindsManifestAndAnnotatedTag(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	git("init", "--initial-branch=main")
	git("config", "user.email", "ci@example.test")
	git("config", "user.name", "CI")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("release\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "-m", "release")
	commitBytes, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := string(commitBytes[:len(commitBytes)-1])
	git("tag", "-a", "v1.2.3", "-m", "qualified")
	manifestPath := filepath.Join(t.TempDir(), "qualification.json")
	manifest := `{"release":{"tag":"v1.2.3","commit":"` + commit + `"}}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(repo, "v1.2.3", commit, "production", manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := Verify(repo, "v1.2.3", commit, "production", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("production selection accepted without qualification manifest")
	}
}

func TestVerifyDevelopmentAndCIChannelsAreExplicit(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	if err := Verify("", "main", "refs/heads/main", "development", ""); err != nil {
		t.Fatal(err)
	}
	if err := Verify("", commit, commit, "ci", ""); err != nil {
		t.Fatal(err)
	}
	if err := Verify("", commit, commit, "production", ""); err == nil {
		t.Fatal("production channel accepted a direct commit")
	}
}
