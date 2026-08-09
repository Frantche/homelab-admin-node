package converge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Options struct {
	RepoDir                 string
	InventoryPath           string
	PlaybookPath            string
	LockFile                string
	SkipGitPull             bool
	ExtraArgs               []string
	ReleaseRefFile          string
	RevisionFile            string
	SchemaSource            string
	SchemaFile              string
	BuildScript             string
	BinaryPath              string
	RequirementsPath        string
	CollectionsRoot         string
	PackageSnapshotSource   string
	PackageSnapshotFile     string
	PackageSnapshotModeFile string
}

type RestartRequiredError struct {
	BinaryPath string
}

func (e *RestartRequiredError) Error() string {
	return fmt.Sprintf("admin-node was rebuilt from the selected release; restart with %s", e.BinaryPath)
}

func Run(ctx context.Context, opts Options) error {
	if err := validateOptions(opts); err != nil {
		return err
	}
	if opts.LockFile == "" {
		opts.LockFile = "/run/admin-converge.lock"
	}
	if err := os.MkdirAll("/run", 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(opts.LockFile, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK {
			fmt.Println("[admin-converge] another run is in progress, exiting")
			return nil
		}
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	fmt.Println("[admin-converge] lock acquired")
	if stat, err := os.Stat(opts.RepoDir + "/.git"); err != nil || !stat.IsDir() {
		return fmt.Errorf("git repository not found in %s", opts.RepoDir)
	}
	if err := updateAdminRepository(ctx, opts, !opts.SkipGitPull); err != nil {
		return err
	}
	if _, err := readSchemaVersion(opts.SchemaSource); err != nil {
		return err
	}
	if err := validatePackageSnapshot(opts); err != nil {
		return err
	}
	rebuilt, err := rebuildAdminNode(ctx, opts)
	if err != nil {
		return err
	}
	if rebuilt {
		return &RestartRequiredError{BinaryPath: opts.BinaryPath}
	}
	if !opts.SkipGitPull {
		inventoryRepo, err := gitRootForPath(ctx, opts.InventoryPath)
		if err != nil {
			return fmt.Errorf("resolve inventory git repository: %w", err)
		}
		if inventoryRepo == "" {
			fmt.Println("[admin-converge] inventory is not inside a git repository, skipping inventory git update")
		} else if samePath(inventoryRepo, opts.RepoDir) {
			fmt.Printf("[admin-converge] inventory git repository already updated in %s\n", inventoryRepo)
		} else {
			if err := updateGitRepository(ctx, inventoryRepo, "inventory repo"); err != nil {
				return err
			}
		}
	} else {
		fmt.Println("[admin-converge] skipping inventory git pull; admin release pin was verified locally")
	}
	if _, err := os.Stat(opts.PlaybookPath); err != nil {
		return fmt.Errorf("playbook not found: %s", opts.PlaybookPath)
	}
	if _, err := os.Stat(opts.InventoryPath); err != nil {
		return fmt.Errorf("inventory not found: %s", opts.InventoryPath)
	}
	collectionsPath, err := prepareAnsibleCollections(ctx, opts)
	if err != nil {
		return err
	}
	args := append([]string{"-i", opts.InventoryPath, opts.PlaybookPath}, opts.ExtraArgs...)
	if err := runWithEnv(ctx, "", []string{"ANSIBLE_COLLECTIONS_PATH=" + collectionsPath}, "ansible-playbook", args...); err != nil {
		return err
	}
	if err := persistInstalledState(ctx, opts); err != nil {
		return err
	}
	fmt.Println("[admin-converge] completed")
	return nil
}

func validateOptions(opts Options) error {
	required := map[string]string{
		"repository":               opts.RepoDir,
		"release pin":              opts.ReleaseRefFile,
		"revision state":           opts.RevisionFile,
		"schema source":            opts.SchemaSource,
		"schema state":             opts.SchemaFile,
		"build script":             opts.BuildScript,
		"binary":                   opts.BinaryPath,
		"Ansible requirements":     opts.RequirementsPath,
		"Ansible collections root": opts.CollectionsRoot,
		"package snapshot source":  opts.PackageSnapshotSource,
		"package snapshot state":   opts.PackageSnapshotFile,
		"package snapshot mode":    opts.PackageSnapshotModeFile,
	}
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s path is required for fail-closed convergence", label)
		}
	}
	return nil
}

func rebuildAdminNode(ctx context.Context, opts Options) (bool, error) {
	output, err := exec.CommandContext(ctx, opts.BuildScript).CombinedOutput()
	if len(output) > 0 {
		fmt.Print(string(output))
	}
	if err != nil {
		return false, fmt.Errorf("build admin-node from selected release: %w", err)
	}
	return strings.Contains(string(output), "changed=true"), nil
}

func validatePackageSnapshot(opts Options) error {
	required, err := readPackageSnapshot(opts.PackageSnapshotSource, false)
	if err != nil {
		return fmt.Errorf("read release package snapshot: %w", err)
	}
	installed, err := readPackageSnapshot(opts.PackageSnapshotFile, true)
	if err != nil {
		return fmt.Errorf("read installed package snapshot: %w", err)
	}
	modeBytes, err := os.ReadFile(opts.PackageSnapshotModeFile)
	if err != nil {
		return fmt.Errorf("read package snapshot mode %s: %w", opts.PackageSnapshotModeFile, err)
	}
	mode := strings.TrimSpace(string(modeBytes))
	if installed == "live" {
		if mode != "ci-live" {
			return fmt.Errorf("live Arch package repositories are forbidden outside the explicit ci-live bootstrap mode")
		}
		fmt.Println("[admin-converge] CI live package mirror explicitly selected; production snapshot equivalence is not claimed")
		return nil
	}
	if mode != "qualified" {
		return fmt.Errorf("unsupported package snapshot mode %q", mode)
	}
	if installed != required {
		return fmt.Errorf("installed Arch package snapshot %s does not match release snapshot %s; refusing an in-place upgrade, rebuild or restore from the qualified snapshot", installed, required)
	}
	return nil
}

func readPackageSnapshot(path string, allowLive bool) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	value := strings.TrimSpace(string(content))
	if allowLive && value == "live" {
		return value, nil
	}
	parsed, err := time.Parse("2006/01/02", value)
	if err != nil || parsed.Format("2006/01/02") != value {
		return "", fmt.Errorf("%s must contain a valid YYYY/MM/DD snapshot", path)
	}
	return value, nil
}

func prepareAnsibleCollections(ctx context.Context, opts Options) (string, error) {
	requirements, err := os.ReadFile(opts.RequirementsPath)
	if err != nil {
		return "", fmt.Errorf("read Ansible collection requirements: %w", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(requirements))
	target := filepath.Join(opts.CollectionsRoot, digest)
	marker := filepath.Join(target, ".complete")
	if content, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(content)) == digest {
		fmt.Printf("[admin-converge] Ansible collections already prepared for %s\n", digest)
		return target, nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("create Ansible collections directory: %w", err)
	}
	if err := run(ctx, "", "ansible-galaxy", "collection", "install", "--force", "-r", opts.RequirementsPath, "-p", target); err != nil {
		return "", fmt.Errorf("install exact Ansible collections for selected release: %w", err)
	}
	if err := writeStateFile(marker, digest+"\n"); err != nil {
		return "", fmt.Errorf("mark Ansible collections prepared: %w", err)
	}
	return target, nil
}

func updateAdminRepository(ctx context.Context, opts Options, allowUpdate bool) error {
	desiredBytes, err := os.ReadFile(opts.ReleaseRefFile)
	if os.IsNotExist(err) {
		return fmt.Errorf(
			"release pin %s is missing; install a full qualified commit SHA or explicitly select main as the development channel",
			opts.ReleaseRefFile,
		)
	}
	if err != nil {
		return fmt.Errorf("read release pin %s: %w", opts.ReleaseRefFile, err)
	}
	desired := strings.TrimSpace(string(desiredBytes))
	if desired == "" {
		return fmt.Errorf("release pin %s is empty", opts.ReleaseRefFile)
	}
	if desired == "main" || desired == "refs/heads/main" {
		fmt.Println("[admin-converge] development channel main selected")
		if !allowUpdate {
			branch, err := commandOutput(ctx, opts.RepoDir, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
			if err != nil || branch != "main" {
				return fmt.Errorf("development channel main is selected but the local checkout is not on main; rerun without --skip-git-pull")
			}
			return nil
		}
		if err := run(ctx, opts.RepoDir, "git", "fetch", "origin", "main"); err != nil {
			return fmt.Errorf("fetch development channel main: %w", err)
		}
		if err := exec.CommandContext(ctx, "git", "-C", opts.RepoDir, "show-ref", "--verify", "--quiet", "refs/heads/main").Run(); err == nil {
			if err := run(ctx, opts.RepoDir, "git", "checkout", "main"); err != nil {
				return fmt.Errorf("checkout existing development channel main: %w", err)
			}
		} else if err := run(ctx, opts.RepoDir, "git", "checkout", "--track", "-b", "main", "origin/main"); err != nil {
			return fmt.Errorf("create development channel main: %w", err)
		}
		if err := run(ctx, opts.RepoDir, "git", "branch", "--set-upstream-to", "origin/main", "main"); err != nil {
			return fmt.Errorf("track development channel main: %w", err)
		}
		return updateGitRepository(ctx, opts.RepoDir, "admin repo")
	}
	if !isFullGitRevision(desired) {
		return fmt.Errorf("release pin %s must contain a full 40-character commit SHA, got %q", opts.ReleaseRefFile, desired)
	}
	current, err := commandOutput(ctx, opts.RepoDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("read installed admin revision: %w", err)
	}
	_, branchErr := commandOutput(ctx, opts.RepoDir, "git", "symbolic-ref", "--quiet", "HEAD")
	if current == desired && branchErr != nil {
		if err := requireCleanReleaseCheckout(ctx, opts.RepoDir); err != nil {
			return err
		}
		fmt.Printf("[admin-converge] admin repo pinned at %s\n", desired)
		return nil
	}
	if !allowUpdate {
		return fmt.Errorf("admin repo is not detached at release pin %s; rerun without --skip-git-pull", desired)
	}
	resolved, err := commandOutput(ctx, opts.RepoDir, "git", "rev-parse", desired+"^{commit}")
	if err != nil {
		if err := run(ctx, opts.RepoDir, "git", "fetch", "origin", desired); err != nil {
			return fmt.Errorf("fetch pinned admin revision %s: %w", desired, err)
		}
		resolved, err = commandOutput(ctx, opts.RepoDir, "git", "rev-parse", "FETCH_HEAD^{commit}")
	}
	if err != nil || resolved != desired {
		return fmt.Errorf("resolved release revision does not match pin %s", desired)
	}
	if err := run(ctx, opts.RepoDir, "git", "checkout", "--detach", desired); err != nil {
		return fmt.Errorf("checkout pinned admin revision %s: %w", desired, err)
	}
	return requireCleanReleaseCheckout(ctx, opts.RepoDir)
}

func requireCleanReleaseCheckout(ctx context.Context, repoDir string) error {
	status, err := commandOutput(ctx, repoDir, "git", "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect pinned admin checkout: %w", err)
	}
	if status != "" {
		return fmt.Errorf("pinned admin checkout contains local changes; refusing to converge an unqualified tree:\n%s", status)
	}
	return nil
}

func persistInstalledState(ctx context.Context, opts Options) error {
	revision, err := commandOutput(ctx, opts.RepoDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("read installed admin revision: %w", err)
	}
	schemaVersion, err := readSchemaVersion(opts.SchemaSource)
	if err != nil {
		return err
	}
	if err := writeStateFile(opts.SchemaFile, schemaVersion+"\n"); err != nil {
		return fmt.Errorf("persist configuration schema version: %w", err)
	}
	// Write the revision last: it is the marker that this exact checkout
	// completed convergence with the already-persisted schema.
	if err := writeStateFile(opts.RevisionFile, revision+"\n"); err != nil {
		return fmt.Errorf("persist installed admin revision: %w", err)
	}
	return nil
}

func readSchemaVersion(path string) (string, error) {
	schema, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read configuration schema version: %w", err)
	}
	schemaVersion := strings.TrimSpace(string(schema))
	if schemaVersion == "" {
		return "", fmt.Errorf("configuration schema version in %s is empty", path)
	}
	return schemaVersion, nil
}

func writeStateFile(path, content string) error {
	if path == "" {
		return fmt.Errorf("state file path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func isFullGitRevision(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func SplitExtraArgs(raw string) []string {
	return strings.Fields(raw)
}

func updateGitRepository(ctx context.Context, repoDir, label string) error {
	branch, err := commandOutput(ctx, repoDir, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		fmt.Printf("[admin-converge] %s in %s is not on a branch, skipping git update\n", label, repoDir)
		return nil
	}
	upstream, err := commandOutput(ctx, repoDir, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return fmt.Errorf("%s in %s is on branch %s but has no upstream: %w", label, repoDir, branch, err)
	}
	fmt.Printf("[admin-converge] checking %s in %s branch=%s upstream=%s\n", label, repoDir, branch, upstream)
	if err := run(ctx, repoDir, "git", "fetch", "--prune"); err != nil {
		return fmt.Errorf("git fetch failed in %s: %w", repoDir, err)
	}
	local, err := commandOutput(ctx, repoDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("read local git revision in %s: %w", repoDir, err)
	}
	remote, err := commandOutput(ctx, repoDir, "git", "rev-parse", upstream)
	if err != nil {
		return fmt.Errorf("read upstream git revision %s in %s: %w", upstream, repoDir, err)
	}
	if local == remote {
		fmt.Printf("[admin-converge] %s already up to date\n", label)
		return nil
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoDir, "merge-base", "--is-ancestor", "HEAD", upstream).Run(); err == nil {
		fmt.Printf("[admin-converge] %s has new commits available, pulling with --ff-only\n", label)
		if err := run(ctx, repoDir, "git", "pull", "--ff-only"); err != nil {
			return fmt.Errorf("git pull failed in %s: %w", repoDir, err)
		}
		return nil
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoDir, "merge-base", "--is-ancestor", upstream, "HEAD").Run(); err == nil {
		fmt.Printf("[admin-converge] %s is ahead of %s, no pull needed\n", label, upstream)
		return nil
	}
	return fmt.Errorf("%s in %s has diverged from %s; refusing to pull before convergence", label, repoDir, upstream)
}

func gitRootForPath(ctx context.Context, path string) (string, error) {
	dir := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}
	dir, err := nearestExistingDir(dir)
	if err != nil {
		return "", err
	}
	root, err := commandOutput(ctx, dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil
	}
	return root, nil
}

func nearestExistingDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(abs)
		if err == nil {
			if info.IsDir() {
				return abs, nil
			}
			return filepath.Dir(abs), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no existing parent directory for %s", path)
		}
		abs = parent
	}
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return absA == absB
}

func run(ctx context.Context, dir, name string, args ...string) error {
	return runWithEnv(ctx, dir, nil, name, args...)
}

func runWithEnv(ctx context.Context, dir string, extraEnv []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func commandOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
