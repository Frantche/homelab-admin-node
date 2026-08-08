package backup

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/stackscope"
)

const ManifestName = "manifest.json"
const ManifestVersion = 2

type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type OfflineImageArchive struct {
	Source     string `json:"source"`
	ArchiveTag string `json:"archive_tag"`
	ImageID    string `json:"image_id"`
}

const (
	ArtifactProduced = "produced"
	ArtifactDisabled = "disabled"
	ArtifactFailed   = "failed"
)

type ManifestArtifact struct {
	Path     string `json:"path"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	External bool   `json:"external,omitempty"`
}

type Manifest struct {
	Version              int                   `json:"version"`
	ID                   string                `json:"id"`
	CreatedAt            time.Time             `json:"created_at"`
	Hostname             string                `json:"hostname"`
	CLIRevision          string                `json:"cli_revision,omitempty"`
	OfflineImages        bool                  `json:"offline_images"`
	Images               []string              `json:"images,omitempty"`
	OfflineImageArchives []OfflineImageArchive `json:"offline_image_archives,omitempty"`
	ActiveStacks         []string              `json:"active_stacks,omitempty"`
	StackDefinitions     bool                  `json:"stack_definitions,omitempty"`
	RepositoryBundle     bool                  `json:"repository_bundle,omitempty"`
	Artifacts            []ManifestArtifact    `json:"artifacts,omitempty"`
	Consistency          string                `json:"consistency"`
	Complete             bool                  `json:"complete"`
	Files                []ManifestFile        `json:"files"`
}

func WriteManifest(dir string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, ManifestName), data, 0o600)
}

func BuildManifestFiles(root string) ([]ManifestFile, error) {
	var files []ManifestFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() == ManifestName {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported backup entry %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		files = append(files, ManifestFile{Path: filepath.ToSlash(rel), Size: info.Size(), SHA256: fmt.Sprintf("%x", hash.Sum(nil))})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func Verify(dir string) (Manifest, error) {
	manifest, ok, err := ReadManifest(dir)
	if err != nil {
		return Manifest{}, err
	}
	if !ok {
		return Manifest{}, fmt.Errorf("manifest is required")
	}
	if manifest.Version != ManifestVersion || !manifest.Complete {
		return Manifest{}, fmt.Errorf("unsupported or incomplete manifest version %d", manifest.Version)
	}
	if manifest.ID == "" || filepath.Base(manifest.ID) != manifest.ID || strings.Contains(manifest.ID, "..") {
		return Manifest{}, fmt.Errorf("invalid manifest id")
	}
	if manifest.ActiveStacks != nil {
		if err := validateActiveStacks(manifest, dir); err != nil {
			return Manifest{}, err
		}
	}
	if len(manifest.Artifacts) > 0 {
		if err := validateManifestArtifacts(manifest, dir); err != nil {
			return Manifest{}, err
		}
	}
	actual, err := BuildManifestFiles(dir)
	if err != nil {
		return Manifest{}, err
	}
	if len(actual) != len(manifest.Files) {
		return Manifest{}, fmt.Errorf("manifest file count mismatch")
	}
	for i := range actual {
		if actual[i] != manifest.Files[i] {
			return Manifest{}, fmt.Errorf("checksum mismatch for %s", actual[i].Path)
		}
	}
	return manifest, nil
}

func validateManifestArtifacts(manifest Manifest, dir string) error {
	seen := map[string]struct{}{}
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == "" || filepath.IsAbs(artifact.Path) || filepath.Clean(artifact.Path) != artifact.Path || strings.HasPrefix(artifact.Path, "..") {
			return fmt.Errorf("invalid manifest artifact path %q", artifact.Path)
		}
		if _, ok := seen[artifact.Path]; ok {
			return fmt.Errorf("duplicate manifest artifact %q", artifact.Path)
		}
		seen[artifact.Path] = struct{}{}
		switch artifact.Status {
		case ArtifactProduced:
			if artifact.External {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(artifact.Path))); err != nil {
				return fmt.Errorf("produced artifact is missing: %s", artifact.Path)
			}
		case ArtifactDisabled:
			if artifact.Required {
				return fmt.Errorf("required artifact is disabled: %s", artifact.Path)
			}
		case ArtifactFailed:
			if manifest.Complete {
				return fmt.Errorf("complete manifest contains failed artifact: %s", artifact.Path)
			}
		default:
			return fmt.Errorf("invalid status %q for artifact %s", artifact.Status, artifact.Path)
		}
	}
	return nil
}

func validateActiveStacks(manifest Manifest, dir string) error {
	if !manifest.StackDefinitions {
		return fmt.Errorf("active stacks require rendered stack definitions")
	}
	if err := stackscope.Validate(manifest.ActiveStacks); err != nil {
		return err
	}
	for _, name := range manifest.ActiveStacks {
		compose := filepath.Join(dir, "stack-definitions", name, "compose.yaml")
		info, err := os.Stat(compose)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("active stack definition is missing: %s", name)
		}
	}
	return nil
}

func ReadManifest(dir string) (Manifest, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, false, nil
		}
		return Manifest{}, false, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, true, err
	}
	return manifest, true, nil
}
