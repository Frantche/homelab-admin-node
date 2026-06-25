package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

type RunOptions struct {
	IncludeImages bool
	Validate      func(context.Context) error
	Now           func() time.Time
}

func Run(ctx context.Context, cfg config.Config, opts RunOptions) (Info, error) {
	mode, _ := os.ReadFile(cfg.ModeFile)
	if strings.TrimSpace(string(mode)) == "locked" || len(mode) == 0 {
		return Info{}, fmt.Errorf("refusing backup in locked mode")
	}
	if opts.Validate != nil {
		if err := opts.Validate(ctx); err != nil {
			return Info{}, err
		}
	}

	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	stamp := now().Format("20060102-150405")
	target := filepath.Join(cfg.BackupRoot, stamp)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return Info{}, err
	}

	if err := runToFile(ctx, filepath.Join(target, "keycloak.sql"), "docker", "exec", "keycloak-db", "pg_dump", "-U", "keycloak", "keycloak"); err != nil {
		return Info{}, fmt.Errorf("dump keycloak: %w", err)
	}

	if containerExists(ctx, "gitea-db") {
		if err := runToFile(ctx, filepath.Join(target, "gitea.sql"), "docker", "exec", "gitea-db", "pg_dump", "-U", "gitea", "gitea"); err != nil {
			return Info{}, fmt.Errorf("dump gitea: %w", err)
		}
	}

	if token := openBaoToken(cfg); token != "" {
		if err := run(ctx, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN="+token, "openbao", "bao", "operator", "raft", "snapshot", "save", "/tmp/openbao.snap"); err != nil {
			return Info{}, fmt.Errorf("openbao snapshot save: %w", err)
		}
		if err := run(ctx, "docker", "cp", "openbao:/tmp/openbao.snap", filepath.Join(target, "openbao.snap")); err != nil {
			return Info{}, fmt.Errorf("openbao snapshot copy: %w", err)
		}
	}

	if err := copyPath(filepath.Join(cfg.AdminRoot, "stacks"), filepath.Join(target, "stacks")); err != nil {
		return Info{}, fmt.Errorf("copy stacks: %w", err)
	}
	if err := copyPath(filepath.Join(cfg.AdminRoot, "env"), filepath.Join(target, "env")); err != nil {
		return Info{}, fmt.Errorf("copy env: %w", err)
	}
	giteaData := filepath.Join(cfg.AdminRoot, "data/gitea")
	if dirExists(giteaData) {
		if err := copyPath(giteaData, filepath.Join(target, "gitea-data")); err != nil {
			return Info{}, fmt.Errorf("copy gitea data: %w", err)
		}
	}

	var images []string
	if opts.IncludeImages {
		detected, err := DetectImages(ctx, cfg.AdminRoot)
		if err != nil {
			return Info{}, fmt.Errorf("detect images: %w", err)
		}
		images = detected
		if len(images) > 0 {
			args := append([]string{"save", "-o", filepath.Join(target, "offline-images.tar")}, images...)
			if err := run(ctx, "docker", args...); err != nil {
				return Info{}, fmt.Errorf("export docker images: %w", err)
			}
		}
	}

	manifest := Manifest{
		Version:       1,
		ID:            stamp,
		CreatedAt:     now().UTC(),
		Hostname:      hostname(),
		OfflineImages: opts.IncludeImages,
		Images:        images,
		Files:         manifestFiles(target),
	}
	if err := WriteManifest(target, manifest); err != nil {
		return Info{}, fmt.Errorf("write manifest: %w", err)
	}

	resticScript := filepath.Join(cfg.RepoRoot, "scripts/restic-backup-repositories.sh")
	if fileExists(resticScript) {
		cmd := exec.CommandContext(ctx, resticScript)
		cmd.Env = append(os.Environ(), "RESTIC_BACKUP_PATHS="+filepath.Join(cfg.AdminRoot, "stacks")+" "+filepath.Join(cfg.AdminRoot, "env")+" "+filepath.Join(cfg.AdminRoot, "data"))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return Info{}, fmt.Errorf("restic backup: %w", err)
		}
	}

	if err := rotateLocal(cfg.BackupRoot, 3); err != nil {
		return Info{}, err
	}
	info, err := inspect(target, stamp)
	if err != nil {
		return Info{}, err
	}
	return info, nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runToFile(ctx context.Context, path string, name string, args ...string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = file
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func containerExists(ctx context.Context, name string) bool {
	cmd := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func openBaoToken(cfg config.Config) string {
	if token := os.Getenv("OPENBAO_TOKEN"); token != "" {
		return token
	}
	for _, path := range []string{
		filepath.Join(cfg.RepoRoot, "secrets/openbao-root-token"),
		"/opt/homelab-admin-node/secrets/openbao-root-token",
	} {
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return copyFile(src, dst, info.Mode())
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func manifestFiles(dir string) []string {
	names := []string{"keycloak.sql", "gitea.sql", "openbao.snap", "stacks", "env", "gitea-data", "offline-images.tar"}
	var present []string
	for _, name := range names {
		if fileExists(filepath.Join(dir, name)) || dirExists(filepath.Join(dir, name)) {
			present = append(present, name)
		}
	}
	return present
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

func rotateLocal(root string, keep int) error {
	backups, err := List(root)
	if err != nil {
		return err
	}
	if len(backups) <= keep {
		return nil
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	for _, item := range backups[keep:] {
		if err := os.RemoveAll(item.Path); err != nil {
			return err
		}
	}
	return nil
}
