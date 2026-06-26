package converge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
		fmt.Printf("[admin-converge] updating git repository in %s\n", opts.RepoDir)
		if err := run(ctx, opts.RepoDir, "git", "pull", "--ff-only"); err != nil {
			return fmt.Errorf("git pull failed in %s: %w", opts.RepoDir, err)
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

func run(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
