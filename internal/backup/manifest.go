package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const ManifestName = "manifest.json"

type Manifest struct {
	Version       int       `json:"version"`
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	Hostname      string    `json:"hostname"`
	CLIRevision   string    `json:"cli_revision,omitempty"`
	OfflineImages bool      `json:"offline_images"`
	Images        []string  `json:"images,omitempty"`
	Files         []string  `json:"files,omitempty"`
}

func WriteManifest(dir string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, ManifestName), data, 0o644)
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
