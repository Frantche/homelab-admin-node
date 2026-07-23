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
)

const ManifestName = "manifest.json"
const ManifestVersion = 2

type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version       int            `json:"version"`
	ID            string         `json:"id"`
	CreatedAt     time.Time      `json:"created_at"`
	Hostname      string         `json:"hostname"`
	CLIRevision   string         `json:"cli_revision,omitempty"`
	OfflineImages bool           `json:"offline_images"`
	Images        []string       `json:"images,omitempty"`
	Consistency   string         `json:"consistency"`
	Complete      bool           `json:"complete"`
	Files         []ManifestFile `json:"files"`
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
