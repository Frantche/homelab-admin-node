package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/mode"
	"github.com/Frantche/homelab-admin-node/internal/operation"
)

type RunOptions struct {
	IncludeImages bool
	Validate      func(context.Context) error
	Now           func() time.Time
}

func Run(ctx context.Context, cfg config.Config, opts RunOptions) (Info, error) {
	currentMode, err := mode.Read(cfg.ModeFile)
	if err != nil || currentMode != "normal" {
		return Info{}, fmt.Errorf("refusing backup unless mode is normal")
	}
	unlock, err := operation.Acquire(cfg.OperationLock)
	if err != nil {
		return Info{}, err
	}
	defer unlock()
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
	if filepath.Base(stamp) != stamp {
		return Info{}, fmt.Errorf("invalid backup id")
	}
	if err := os.MkdirAll(cfg.BackupRoot, 0o700); err != nil {
		return Info{}, err
	}
	if err := os.Chmod(cfg.BackupRoot, 0o700); err != nil {
		return Info{}, err
	}
	partial, err := os.MkdirTemp(cfg.BackupRoot, ".partial-"+stamp+"-")
	if err != nil {
		return Info{}, err
	}
	if err := os.Chmod(partial, 0o700); err != nil {
		return Info{}, err
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.RemoveAll(partial)
		}
	}()
	if _, err := os.Stat(target); err == nil {
		return Info{}, fmt.Errorf("backup already exists: %s", stamp)
	}

	if err := dumpPostgres(ctx, partial, "keycloak.dump", "keycloak-db", "keycloak", "keycloak"); err != nil {
		return Info{}, fmt.Errorf("dump keycloak: %w", err)
	}

	if containerExists(ctx, "gitea-db") {
		if err := dumpPostgres(ctx, partial, "gitea.dump", "gitea-db", "gitea", "gitea"); err != nil {
			return Info{}, fmt.Errorf("dump gitea: %w", err)
		}
	}

	if containerExists(ctx, "harbor-db") {
		if err := backupHarbor(ctx, cfg, partial, stamp); err != nil {
			return Info{}, err
		}
	}

	if token := openBaoToken(cfg); token != "" {
		if err := runWithEnv(ctx, []string{"VAULT_TOKEN=" + token}, "docker", "exec", "-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN", "openbao", "bao", "operator", "raft", "snapshot", "save", "/tmp/openbao.snap"); err != nil {
			return Info{}, fmt.Errorf("openbao snapshot save: %w", err)
		}
		if err := run(ctx, "docker", "cp", "openbao:/tmp/openbao.snap", filepath.Join(partial, "openbao.snap")); err != nil {
			return Info{}, fmt.Errorf("openbao snapshot copy: %w", err)
		}
	}

	consistency := "logical-online"
	if dirExists(cfg.GiteaStackPath) {
		usedSnapshot, err := copyBtrfsSnapshot(ctx, cfg.GiteaStackPath, cfg.SnapshotRoot, stamp, filepath.Join(partial, "gitea-stack"))
		if err != nil {
			return Info{}, fmt.Errorf("snapshot gitea stack: %w", err)
		}
		if usedSnapshot {
			consistency = "btrfs-atomic-crash-consistent"
		} else if cfg.RequireBtrfsHotBackup {
			return Info{}, fmt.Errorf("%s is not a Btrfs subvolume", cfg.GiteaStackPath)
		} else if err := copyPath(cfg.GiteaStackPath, filepath.Join(partial, "gitea-stack")); err != nil {
			return Info{}, err
		}
	} else {
		giteaData := filepath.Join(cfg.AdminRoot, "data/gitea")
		if cfg.RequireBtrfsHotBackup {
			return Info{}, fmt.Errorf("required Gitea stack subvolume is missing: %s", cfg.GiteaStackPath)
		}
		if dirExists(giteaData) {
			if err := copyPath(giteaData, filepath.Join(partial, "gitea-data")); err != nil {
				return Info{}, fmt.Errorf("copy gitea data: %w", err)
			}
		}
	}

	images, err := DetectImages(ctx, cfg.AdminRoot)
	if err != nil {
		return Info{}, fmt.Errorf("detect images: %w", err)
	}
	if opts.IncludeImages {
		if len(images) > 0 {
			args := append([]string{"save", "-o", filepath.Join(partial, "offline-images.tar")}, images...)
			if err := run(ctx, "docker", args...); err != nil {
				return Info{}, fmt.Errorf("export docker images: %w", err)
			}
		}
	}

	files, err := BuildManifestFiles(partial)
	if err != nil {
		return Info{}, fmt.Errorf("build manifest: %w", err)
	}
	manifest := Manifest{Version: ManifestVersion, ID: stamp, CreatedAt: now().UTC(), Hostname: hostname(), CLIRevision: repoRevision(ctx, cfg.RepoRoot), OfflineImages: opts.IncludeImages, Images: images, Consistency: consistency, Complete: true, Files: files}
	if err := WriteManifest(partial, manifest); err != nil {
		return Info{}, fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(partial, target); err != nil {
		return Info{}, fmt.Errorf("publish backup: %w", err)
	}
	completed = true

	if err := RunRestic(ctx, cfg.BackupEnvFile, []string{target}); err != nil {
		return Info{}, fmt.Errorf("restic backup: %w", err)
	}
	retention := cfg.LocalBackupRetention
	if retention < 1 {
		retention = 3
	}
	if err := rotateLocal(cfg.BackupRoot, retention); err != nil {
		return Info{}, err
	}
	return inspect(target, stamp)
}

func repoRevision(ctx context.Context, repoRoot string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runWithEnv(ctx context.Context, extraEnv []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runToFile(ctx context.Context, path string, name string, args ...string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func dumpPostgres(ctx context.Context, target string, filename string, container string, user string, db string) error {
	return runToFile(ctx, filepath.Join(target, filename), "docker", "exec", container, "pg_dump", "-Fc", "-U", user, "-d", db)
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
		filepath.Join(cfg.AdminRoot, "env/openbao-backup-token"),
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
	root, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return copyPathWithin(root, src, dst)
}

func copyPathWithin(root, src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(src)
		if err != nil {
			return err
		}
		resolvedAbs, err := filepath.Abs(resolved)
		if err != nil {
			return err
		}
		if resolvedAbs != root && !strings.HasPrefix(resolvedAbs, root+string(os.PathSeparator)) {
			return fmt.Errorf("symlink escapes backup source: %s", src)
		}
		return copyPathWithin(root, resolvedAbs, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()&0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPathWithin(root, filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported backup source: %s", src)
	}
	return copyFile(src, dst, info.Mode().Perm()&0o600)
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
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

func copyBtrfsSnapshot(ctx context.Context, source, snapshotRoot, id, dst string) (bool, error) {
	return copyBtrfsSnapshotPaths(ctx, source, snapshotRoot, id, "gitea", map[string]string{"": dst})
}

func copyBtrfsSnapshotPaths(ctx context.Context, source, snapshotRoot, id, label string, paths map[string]string) (bool, error) {
	if _, err := exec.LookPath("btrfs"); err != nil {
		return false, nil
	}
	if err := exec.CommandContext(ctx, "btrfs", "subvolume", "show", source).Run(); err != nil {
		return false, nil
	}
	if err := os.MkdirAll(snapshotRoot, 0o700); err != nil {
		return false, err
	}
	snapshot := filepath.Join(snapshotRoot, "."+label+"-"+id+fmt.Sprintf("-%d", os.Getpid()))
	if err := run(ctx, "btrfs", "subvolume", "snapshot", "-r", source, snapshot); err != nil {
		return false, err
	}
	defer func() { _ = exec.Command("btrfs", "subvolume", "delete", snapshot).Run() }()
	for rel, dst := range paths {
		src := filepath.Join(snapshot, filepath.Clean(rel))
		if !dirExists(src) && !fileExists(src) {
			continue
		}
		if dirExists(src) {
			if err := os.MkdirAll(dst, 0o700); err != nil {
				return false, err
			}
			if err := run(ctx, "cp", "-a", "--reflink=always", directoryContentsPath(src), dst); err != nil {
				return false, fmt.Errorf("reflink snapshot %s: %w", rel, err)
			}
		} else if err := copyPath(src, dst); err != nil {
			return false, err
		}
	}
	return true, nil
}

func directoryContentsPath(path string) string {
	return filepath.Clean(path) + string(os.PathSeparator) + "."
}

func backupHarbor(ctx context.Context, cfg config.Config, target, id string) (err error) {
	user, password := os.Getenv("HARBOR_ADMIN_USER"), os.Getenv("HARBOR_ADMIN_PASSWORD")
	readOnly := user != "" && password != ""
	if cfg.RequireHarborReadOnly && !readOnly {
		return fmt.Errorf("harbor read-only credentials are required")
	}
	if readOnly {
		if err := SetHarborReadOnly(ctx, cfg.HarborDomain, user, password, true); err != nil {
			return fmt.Errorf("enable harbor read-only mode: %w", err)
		}
		defer func() {
			if resetErr := SetHarborReadOnly(context.Background(), cfg.HarborDomain, user, password, false); resetErr != nil && err == nil {
				err = fmt.Errorf("disable harbor read-only mode: %w", resetErr)
			}
		}()
	}
	if err := dumpPostgres(ctx, target, "harbor.dump", "harbor-db", "postgres", "registry"); err != nil {
		return fmt.Errorf("dump harbor: %w", err)
	}
	harborPath := filepath.Join(cfg.AdminRoot, "data/harbor")
	if dirExists(harborPath) {
		paths := map[string]string{
			"registry":              filepath.Join(target, "harbor-data/registry"),
			"core":                  filepath.Join(target, "harbor-data/core"),
			"job_logs":              filepath.Join(target, "harbor-data/job_logs"),
			"trivy-adapter/reports": filepath.Join(target, "harbor-data/trivy-adapter/reports"),
		}
		used, snapErr := copyBtrfsSnapshotPaths(ctx, harborPath, cfg.SnapshotRoot, id, "harbor", paths)
		if snapErr != nil {
			return fmt.Errorf("snapshot harbor data: %w", snapErr)
		}
		if !used && cfg.RequireBtrfsHotBackup {
			return fmt.Errorf("%s is not a Btrfs subvolume", harborPath)
		}
		if !used {
			for rel, dst := range paths {
				if dirExists(filepath.Join(harborPath, rel)) {
					if err := copyPath(filepath.Join(harborPath, rel), dst); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func SetHarborReadOnly(ctx context.Context, domain, user, password string, enabled bool) error {
	body, err := json.Marshal(map[string]bool{"read_only": enabled})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "https://"+domain+"/api/v2.0/configurations", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("harbor API returned %s", resp.Status)
	}
	return nil
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
