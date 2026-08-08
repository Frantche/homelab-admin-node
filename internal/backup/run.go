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
	"github.com/Frantche/homelab-admin-node/internal/stackscope"
)

type RunOptions struct {
	IncludeImages bool
	Validate      func(context.Context) error
	Now           func() time.Time
}

const openBaoSnapshotContainerPath = "/openbao/snapshot/openbao.snap"

func Run(ctx context.Context, cfg config.Config, opts RunOptions) (Info, error) {
	currentMode, err := mode.Read(cfg.ModeFile)
	if err != nil || currentMode != "normal" {
		return Info{}, fmt.Errorf("refusing backup unless mode is normal")
	}
	unlock, err := operation.AcquireWait(ctx, cfg.OperationLock, cfg.BackupOperationLockTimeout)
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
	activeStacks, err := stackscope.Discover(cfg)
	if err != nil {
		return Info{}, fmt.Errorf("discover active stacks: %w", err)
	}
	if len(activeStacks) == 0 {
		return Info{}, fmt.Errorf("no active rendered stacks found")
	}
	runSuccessful := false
	defer func() {
		if !runSuccessful {
			artifactRoot := partial
			if completed {
				artifactRoot = target
			}
			_ = writeArtifactFailureRecord(cfg.BackupRoot, artifactRoot, stamp, activeStacks, opts.IncludeImages)
		}
	}()

	cliRevision := repoRevision(ctx, cfg.RepoRoot)
	if opts.IncludeImages {
		if cliRevision == "" {
			return Info{}, fmt.Errorf("include docker images: repository HEAD revision is unavailable")
		}
		status, err := commandOutput(ctx, "git", "-C", cfg.RepoRoot, "status", "--porcelain", "--untracked-files=no")
		if err != nil {
			return Info{}, fmt.Errorf("inspect repository state: %w", err)
		}
		if status != "" {
			return Info{}, fmt.Errorf("include docker images: repository has tracked changes and cannot be tied to an exact revision")
		}
		bundlePath := filepath.Join(partial, "repository.bundle")
		if err := run(ctx, "git", "-C", cfg.RepoRoot, "bundle", "create", bundlePath, "HEAD"); err != nil {
			return Info{}, fmt.Errorf("create repository bundle for revision %s: %w", cliRevision, err)
		}
		if err := os.Chmod(bundlePath, 0o600); err != nil {
			return Info{}, err
		}
	}

	if stackscope.Contains(activeStacks, "keycloak") {
		if err := dumpPostgres(ctx, partial, "keycloak.dump", "keycloak-db", "keycloak", "keycloak"); err != nil {
			return Info{}, fmt.Errorf("dump keycloak: %w", err)
		}
	}

	if stackscope.Contains(activeStacks, "gitea") {
		if !containerExists(ctx, "gitea-db") {
			return Info{}, fmt.Errorf("required Gitea database container gitea-db is not running")
		}
		if err := dumpPostgres(ctx, partial, "gitea.dump", "gitea-db", "gitea", "gitea"); err != nil {
			return Info{}, fmt.Errorf("dump gitea: %w", err)
		}
	}

	if stackscope.Contains(activeStacks, "harbor") {
		if !containerExists(ctx, "harbor-db") {
			return Info{}, fmt.Errorf("required Harbor database container harbor-db is not running")
		}
		if err := backupHarbor(ctx, cfg, partial, stamp); err != nil {
			return Info{}, err
		}
	}

	if stackscope.Contains(activeStacks, "openbao") {
		token := openBaoToken(cfg)
		if token == "" {
			return Info{}, fmt.Errorf("OpenBao is active but no snapshot token is available")
		}
		scratchPath, err := openBaoSnapshotScratchPath(cfg.AdminRoot)
		if err != nil {
			return Info{}, err
		}
		if err := os.Remove(scratchPath); err != nil && !os.IsNotExist(err) {
			return Info{}, fmt.Errorf("remove stale openbao snapshot scratch: %w", err)
		}
		defer os.Remove(scratchPath)
		if err := runWithEnv(ctx, []string{"VAULT_TOKEN=" + token}, "docker", "exec", "-e", "BAO_ADDR=https://127.0.0.1:8200", "-e", "BAO_CACERT=/openbao/tls/ca.pem", "-e", "VAULT_TOKEN", "openbao", "sh", "-c", "umask 077; exec bao operator raft snapshot save "+openBaoSnapshotContainerPath); err != nil {
			return Info{}, fmt.Errorf("openbao snapshot save: %w", err)
		}
		info, err := os.Stat(scratchPath)
		if err != nil {
			return Info{}, fmt.Errorf("inspect openbao snapshot scratch: %w", err)
		}
		if info.Size() == 0 {
			return Info{}, fmt.Errorf("openbao snapshot scratch is empty")
		}
		if err := os.Chmod(scratchPath, 0o600); err != nil {
			return Info{}, fmt.Errorf("restrict openbao snapshot scratch permissions: %w", err)
		}
		if err := copyFile(scratchPath, filepath.Join(partial, "openbao.snap"), 0o600); err != nil {
			return Info{}, fmt.Errorf("copy openbao snapshot scratch: %w", err)
		}
		if err := os.Remove(scratchPath); err != nil {
			return Info{}, fmt.Errorf("remove openbao snapshot scratch: %w", err)
		}
	}

	consistency := "logical-online"
	if stackscope.Contains(activeStacks, "gitea") && dirExists(cfg.GiteaStackPath) {
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
	} else if stackscope.Contains(activeStacks, "gitea") {
		giteaData := filepath.Join(cfg.AdminRoot, "data/gitea")
		if cfg.RequireBtrfsHotBackup {
			return Info{}, fmt.Errorf("required Gitea stack subvolume is missing: %s", cfg.GiteaStackPath)
		}
		if dirExists(giteaData) {
			if err := copyPath(giteaData, filepath.Join(partial, "gitea-data")); err != nil {
				return Info{}, fmt.Errorf("copy gitea data: %w", err)
			}
		} else {
			return Info{}, fmt.Errorf("required Gitea filesystem data is missing: %s", giteaData)
		}
	}

	images := DetectImagesForStacks(ctx, cfg.AdminRoot, activeStacks)
	var offlineImageArchives []OfflineImageArchive
	if opts.IncludeImages {
		if len(images) == 0 {
			return Info{}, fmt.Errorf("include docker images: no images were detected for active stacks")
		}
		if len(images) > 0 {
			var archiveTags []string
			for index, image := range images {
				imageID, err := commandOutput(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", image)
				if err != nil {
					return Info{}, fmt.Errorf("inspect docker image %s: %w", image, err)
				}
				archiveTag := fmt.Sprintf("admin-node-backup.local/%s:image-%03d", stamp, index+1)
				if err := run(ctx, "docker", "tag", image, archiveTag); err != nil {
					return Info{}, fmt.Errorf("tag docker image %s for offline archive: %w", image, err)
				}
				defer func(tag string) {
					cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
					defer cancel()
					_ = run(cleanupCtx, "docker", "image", "rm", tag)
				}(archiveTag)
				archiveTags = append(archiveTags, archiveTag)
				offlineImageArchives = append(offlineImageArchives, OfflineImageArchive{
					Source:     image,
					ArchiveTag: archiveTag,
					ImageID:    imageID,
				})
			}
			args := append([]string{"save", "-o", filepath.Join(partial, "offline-images.tar")}, archiveTags...)
			if err := run(ctx, "docker", args...); err != nil {
				return Info{}, fmt.Errorf("export docker images: %w", err)
			}
		}
	}
	if err := copyActiveStackDefinitions(cfg.AdminRoot, partial, activeStacks); err != nil {
		return Info{}, err
	}
	if opts.IncludeImages {
		if err := rewriteStackImageReferences(filepath.Join(partial, "stack-definitions"), offlineImageArchives); err != nil {
			return Info{}, fmt.Errorf("prepare offline stack definitions: %w", err)
		}
	}

	files, err := BuildManifestFiles(partial)
	if err != nil {
		return Info{}, fmt.Errorf("build manifest: %w", err)
	}
	artifacts, err := buildManifestArtifacts(partial, activeStacks, opts.IncludeImages)
	if err != nil {
		return Info{}, err
	}
	manifest := Manifest{
		Version:              ManifestVersion,
		ID:                   stamp,
		CreatedAt:            now().UTC(),
		Hostname:             hostname(),
		CLIRevision:          cliRevision,
		OfflineImages:        opts.IncludeImages,
		Images:               images,
		OfflineImageArchives: offlineImageArchives,
		ActiveStacks:         activeStacks,
		StackDefinitions:     true,
		RepositoryBundle:     opts.IncludeImages,
		Artifacts:            artifacts,
		Consistency:          consistency,
		Complete:             true,
		Files:                files,
	}
	if err := WriteManifest(partial, manifest); err != nil {
		return Info{}, fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(partial, target); err != nil {
		return Info{}, fmt.Errorf("publish backup: %w", err)
	}
	completed = true

	resticCfg, resticConfigErr := loadResticConfig(cfg.BackupEnvFile)
	remoteRequired := resticCfg.RequireRemote
	if resticConfigErr != nil {
		if remoteRequired {
			_ = recordRemoteDelivery(target, ArtifactFailed, false)
		}
		return Info{}, fmt.Errorf("restic backup: %w", resticConfigErr)
	}
	if err := RunRestic(ctx, cfg.BackupEnvFile, []string{target}); err != nil {
		if remoteRequired {
			_ = recordRemoteDelivery(target, ArtifactFailed, false)
		}
		return Info{}, fmt.Errorf("restic backup: %w", err)
	}
	if remoteRequired {
		if err := recordRemoteDelivery(target, ArtifactProduced, true); err != nil {
			return Info{}, fmt.Errorf("record remote delivery: %w", err)
		}
	}
	retention := cfg.LocalBackupRetention
	if retention < 1 {
		retention = 3
	}
	if err := rotateLocal(cfg.BackupRoot, retention); err != nil {
		return Info{}, err
	}
	info, err := inspect(target, stamp)
	if err != nil {
		return Info{}, err
	}
	runSuccessful = true
	return info, nil
}

func writeArtifactFailureRecord(backupRoot, artifactRoot, id string, activeStacks []string, includeImages bool) error {
	artifacts := expectedManifestArtifacts(artifactRoot, activeStacks, includeImages, false)
	failureDir := filepath.Join(backupRoot, ".failed", id)
	if err := os.MkdirAll(failureDir, 0o700); err != nil {
		return err
	}
	manifest := Manifest{Version: ManifestVersion, ID: id, CreatedAt: time.Now().UTC(), Hostname: hostname(), ActiveStacks: activeStacks, Artifacts: artifacts, Complete: false}
	return WriteManifest(failureDir, manifest)
}

func recordRemoteDelivery(root, status string, complete bool) error {
	manifest, ok, err := ReadManifest(root)
	if err != nil || !ok {
		return fmt.Errorf("read local backup manifest before recording delivery")
	}
	manifest.Artifacts = append(manifest.Artifacts, ManifestArtifact{Path: "remote-delivery", Required: true, Status: status, External: true})
	manifest.Complete = complete
	return WriteManifest(root, manifest)
}

func buildManifestArtifacts(root string, activeStacks []string, includeImages bool) ([]ManifestArtifact, error) {
	artifacts := expectedManifestArtifacts(root, activeStacks, includeImages, true)
	manifest := Manifest{Complete: true, Artifacts: artifacts}
	if err := validateManifestArtifacts(manifest, root); err != nil {
		return nil, fmt.Errorf("backup artifact set is incomplete: %w", err)
	}
	return artifacts, nil
}

func expectedManifestArtifacts(root string, activeStacks []string, includeImages, requireProduced bool) []ManifestArtifact {
	var artifacts []ManifestArtifact
	add := func(path string, required bool, status string) {
		if requireProduced && status == ArtifactFailed {
			status = ArtifactProduced
		}
		artifacts = append(artifacts, ManifestArtifact{Path: filepath.ToSlash(path), Required: required, Status: status})
	}
	localStatus := func(path string) string {
		if _, err := os.Stat(filepath.Join(root, path)); err == nil {
			return ArtifactProduced
		}
		return ArtifactFailed
	}
	for _, stack := range activeStacks {
		stackDefinition := filepath.Join("stack-definitions", stack)
		add(stackDefinition, true, localStatus(stackDefinition))
		switch stack {
		case "keycloak":
			add("keycloak.dump", true, localStatus("keycloak.dump"))
		case "gitea":
			add("gitea.dump", true, localStatus("gitea.dump"))
			if dirExists(filepath.Join(root, "gitea-stack")) {
				add("gitea-stack", true, localStatus("gitea-stack"))
			} else {
				add("gitea-data", true, localStatus("gitea-data"))
			}
		case "harbor":
			add("harbor.dump", true, localStatus("harbor.dump"))
			add("harbor-data", true, localStatus("harbor-data"))
		case "openbao":
			add("openbao.snap", true, localStatus("openbao.snap"))
		}
	}
	if includeImages {
		add("offline-images.tar", true, localStatus("offline-images.tar"))
		add("repository.bundle", true, localStatus("repository.bundle"))
	} else {
		add("offline-images.tar", false, ArtifactDisabled)
		add("repository.bundle", false, ArtifactDisabled)
	}
	return artifacts
}

func copyActiveStackDefinitions(adminRoot, partial string, activeStacks []string) error {
	target := filepath.Join(partial, "stack-definitions")
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, name := range activeStacks {
		source := filepath.Join(adminRoot, "stacks", name)
		if err := copyPathPreservingMode(source, filepath.Join(target, name)); err != nil {
			return fmt.Errorf("copy rendered stack definition %s: %w", name, err)
		}
	}
	return nil
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func rewriteStackImageReferences(root string, archives []OfflineImageArchive) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported rendered stack entry: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := string(data)
		for _, archive := range archives {
			updated = strings.ReplaceAll(updated, archive.Source, archive.ArchiveTag)
		}
		if updated == string(data) {
			return nil
		}
		return os.WriteFile(path, []byte(updated), info.Mode().Perm())
	})
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

func openBaoSnapshotScratchPath(adminRoot string) (string, error) {
	if strings.TrimSpace(adminRoot) == "" {
		return "", fmt.Errorf("openbao snapshot scratch requires admin root")
	}
	return filepath.Join(adminRoot, "backups/openbao-scratch/openbao.snap"), nil
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
	return copyPathWithMode(src, dst, false)
}

func copyPathPreservingMode(src, dst string) error {
	return copyPathWithMode(src, dst, true)
}

func copyPathWithMode(src, dst string, preserveMode bool) error {
	root, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return copyPathWithin(root, src, dst, preserveMode)
}

func copyPathWithin(root, src, dst string, preserveMode bool) error {
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
		return copyPathWithin(root, resolvedAbs, dst, preserveMode)
	}
	if info.IsDir() {
		mode := info.Mode().Perm()
		if !preserveMode {
			mode &= 0o700
		}
		if err := os.MkdirAll(dst, mode); err != nil {
			return err
		}
		if err := os.Chmod(dst, mode); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPathWithin(root, filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), preserveMode); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported backup source: %s", src)
	}
	mode := info.Mode().Perm()
	if !preserveMode {
		mode &= 0o600
	}
	return copyFile(src, dst, mode)
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
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
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
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if resetErr := SetHarborReadOnly(cleanupCtx, cfg.HarborDomain, user, password, false); resetErr != nil && err == nil {
				err = fmt.Errorf("disable harbor read-only mode: %w", resetErr)
			}
		}()
	}
	if err := dumpPostgres(ctx, target, "harbor.dump", "harbor-db", "postgres", "registry"); err != nil {
		return fmt.Errorf("dump harbor: %w", err)
	}
	harborPath := filepath.Join(cfg.AdminRoot, "data/harbor")
	if !dirExists(harborPath) {
		return fmt.Errorf("required Harbor filesystem data is missing: %s", harborPath)
	}
	if !dirExists(filepath.Join(harborPath, "registry")) {
		return fmt.Errorf("required Harbor registry data is missing: %s", filepath.Join(harborPath, "registry"))
	}
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
	if !dirExists(filepath.Join(target, "harbor-data/registry")) {
		return fmt.Errorf("required Harbor registry artifact was not produced")
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
