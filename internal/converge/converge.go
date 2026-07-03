package converge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type Options struct {
	RepoDir       string
	InventoryPath string
	PlaybookPath  string
	LockFile      string
	SkipGitPull   bool
	ExtraArgs     []string
}

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
	if !opts.SkipGitPull {
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
		return err
	}
	fmt.Println("[admin-converge] completed")
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
