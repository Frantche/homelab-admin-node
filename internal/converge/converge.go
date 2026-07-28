package converge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

type Options struct {
	RepoDir               string
	InventoryPath         string
	PlaybookPath          string
	LockFile              string
	SkipGitPull           bool
	ExtraArgs             []string
	RequireApproval       bool
	ApprovalFile          string
	InventoryApprovalFile string
	TrustedGNUPGHome      string
	Upstream              string
	InventoryUpstream     string
	LastGoodFile          string
	InventoryLastGoodFile string
}

var fullRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)

func Run(ctx context.Context, opts Options) error {
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
	var prepared []preparedRepository
	if opts.RequireApproval {
		if opts.ApprovalFile == "" || opts.TrustedGNUPGHome == "" {
			return fmt.Errorf("approval file and trusted GNUPG home are required")
		}
		if opts.Upstream == "" {
			opts.Upstream = "origin/main"
		}
		code, err := prepareApprovedRepository(ctx, opts.RepoDir, "admin repo", opts.ApprovalFile, opts.TrustedGNUPGHome, opts.Upstream, !opts.SkipGitPull)
		if err != nil {
			return err
		}
		if err := applyLastKnownGood(ctx, &code, opts.LastGoodFile, opts.Upstream, opts.TrustedGNUPGHome); err != nil {
			return err
		}
		prepared = append(prepared, code)

		inventoryRepo, err := gitRootForPath(ctx, opts.InventoryPath)
		if err != nil {
			return fmt.Errorf("resolve inventory git repository: %w", err)
		}
		if inventoryRepo != "" && !samePath(inventoryRepo, opts.RepoDir) {
			if opts.InventoryApprovalFile == "" {
				return fmt.Errorf("inventory approval file is required for %s", inventoryRepo)
			}
			if opts.InventoryUpstream == "" {
				opts.InventoryUpstream = "origin/main"
			}
			inventory, err := prepareApprovedRepository(ctx, inventoryRepo, "inventory repo", opts.InventoryApprovalFile, opts.TrustedGNUPGHome, opts.InventoryUpstream, !opts.SkipGitPull)
			if err != nil {
				rollbackRepositories(ctx, prepared)
				return err
			}
			if err := applyLastKnownGood(ctx, &inventory, opts.InventoryLastGoodFile, opts.InventoryUpstream, opts.TrustedGNUPGHome); err != nil {
				rollbackRepositories(ctx, prepared)
				return err
			}
			prepared = append(prepared, inventory)
		}
	} else if !opts.SkipGitPull {
		if err := updateGitRepository(ctx, opts.RepoDir, "admin repo"); err != nil {
			return err
		}
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
		fmt.Println("[admin-converge] skipping git pull")
	}
	if _, err := os.Stat(opts.PlaybookPath); err != nil {
		return fmt.Errorf("playbook not found: %s", opts.PlaybookPath)
	}
	if _, err := os.Stat(opts.InventoryPath); err != nil {
		return fmt.Errorf("inventory not found: %s", opts.InventoryPath)
	}
	args := append([]string{"-i", opts.InventoryPath, opts.PlaybookPath}, opts.ExtraArgs...)
	if err := run(ctx, "", "ansible-playbook", args...); err != nil {
		if opts.RequireApproval {
			rollbackErr := rollbackAndConverge(ctx, prepared, args)
			if rollbackErr != nil {
				return fmt.Errorf("convergence failed: %w; rollback failed: %v", err, rollbackErr)
			}
			return fmt.Errorf("convergence failed and previous approved release was restored: %w", err)
		}
		return err
	}
	if opts.RequireApproval && opts.LastGoodFile != "" {
		if err := writeRevisionFile(opts.LastGoodFile, prepared[0].Approved); err != nil {
			return fmt.Errorf("record last known good revision: %w", err)
		}
		if len(prepared) > 1 && opts.InventoryLastGoodFile != "" {
			if err := writeRevisionFile(opts.InventoryLastGoodFile, prepared[1].Approved); err != nil {
				return fmt.Errorf("record inventory last known good revision: %w", err)
			}
		}
	}
	fmt.Println("[admin-converge] completed")
	return nil
}

func applyLastKnownGood(ctx context.Context, repository *preparedRepository, path, upstream, gnupgHome string) error {
	if path == "" {
		return nil
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s last known good revision: %w", repository.Label, err)
	}
	revision := strings.TrimSpace(string(content))
	if !fullRevision.MatchString(revision) {
		return fmt.Errorf("%s last known good file is invalid", repository.Label)
	}
	if err := verifyApprovedRevision(ctx, repository.Dir, revision, upstream, gnupgHome); err != nil {
		return fmt.Errorf("verify %s last known good revision: %w", repository.Label, err)
	}
	repository.Previous = revision
	return nil
}

type preparedRepository struct {
	Dir      string
	Label    string
	Previous string
	Approved string
	Changed  bool
}

func prepareApprovedRepository(ctx context.Context, repoDir, label, approvalFile, gnupgHome, upstream string, fetch bool) (preparedRepository, error) {
	result := preparedRepository{Dir: repoDir, Label: label}
	if dirty, err := commandOutput(ctx, repoDir, "git", "status", "--porcelain", "--untracked-files=no"); err != nil {
		return result, fmt.Errorf("inspect %s tracked state: %w", label, err)
	} else if dirty != "" {
		return result, fmt.Errorf("%s in %s has tracked local changes; refusing trusted convergence", label, repoDir)
	}
	if fetch {
		if err := run(ctx, repoDir, "git", "fetch", "--prune", "origin"); err != nil {
			return result, fmt.Errorf("git fetch failed in %s: %w", repoDir, err)
		}
	}
	approvedBytes, err := os.ReadFile(approvalFile)
	if err != nil {
		return result, fmt.Errorf("read %s approval file: %w", label, err)
	}
	approved := strings.TrimSpace(string(approvedBytes))
	if !fullRevision.MatchString(approved) {
		return result, fmt.Errorf("%s approval must contain one full lowercase commit SHA", label)
	}
	if err := verifyApprovedRevision(ctx, repoDir, approved, upstream, gnupgHome); err != nil {
		return result, fmt.Errorf("verify approved %s revision %s: %w", label, approved, err)
	}
	previous, err := commandOutput(ctx, repoDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return result, fmt.Errorf("read current %s revision: %w", label, err)
	}
	result.Previous = previous
	result.Approved = approved
	if previous == approved {
		return result, nil
	}
	if err := run(ctx, repoDir, "git", "checkout", "--detach", approved); err != nil {
		return result, fmt.Errorf("checkout approved %s revision: %w", label, err)
	}
	result.Changed = true
	return result, nil
}

func verifyApprovedRevision(ctx context.Context, repoDir, revision, upstream, gnupgHome string) error {
	if err := exec.CommandContext(ctx, "git", "-C", repoDir, "cat-file", "-e", revision+"^{commit}").Run(); err != nil {
		return fmt.Errorf("commit is unavailable")
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoDir, "merge-base", "--is-ancestor", revision, upstream).Run(); err != nil {
		return fmt.Errorf("commit is not reachable from %s", upstream)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "verify-commit", revision)
	cmd.Env = append(os.Environ(), "GNUPGHOME="+gnupgHome)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commit signature is not trusted: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func Approve(ctx context.Context, repoDir, revision, approvalFile, upstream, gnupgHome string) (string, error) {
	if err := run(ctx, repoDir, "git", "fetch", "--prune", "origin"); err != nil {
		return "", err
	}
	resolved, err := commandOutput(ctx, repoDir, "git", "rev-parse", revision+"^{commit}")
	if err != nil || !fullRevision.MatchString(resolved) {
		return "", fmt.Errorf("resolve revision %q", revision)
	}
	if err := verifyApprovedRevision(ctx, repoDir, resolved, upstream, gnupgHome); err != nil {
		return "", err
	}
	if err := writeRevisionFile(approvalFile, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func writeRevisionFile(path, revision string) error {
	if !fullRevision.MatchString(revision) {
		return fmt.Errorf("invalid revision %q", revision)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".approved-revision-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return err
	}
	if _, err := fmt.Fprintln(temp, revision); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func rollbackRepositories(ctx context.Context, repositories []preparedRepository) error {
	var failures []string
	for i := len(repositories) - 1; i >= 0; i-- {
		repository := repositories[i]
		if !repository.Changed {
			continue
		}
		if err := run(ctx, repository.Dir, "git", "checkout", "--detach", repository.Previous); err != nil {
			failures = append(failures, repository.Label+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func rollbackAndConverge(ctx context.Context, repositories []preparedRepository, ansibleArgs []string) error {
	if err := rollbackRepositories(ctx, repositories); err != nil {
		return err
	}
	if err := run(ctx, "", "ansible-playbook", ansibleArgs...); err != nil {
		return fmt.Errorf("previous release convergence failed: %w", err)
	}
	return nil
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
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
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
