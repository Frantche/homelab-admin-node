package backup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Info struct {
	ID              string
	Path            string
	CreatedAt       time.Time
	SizeBytes       int64
	HasManifest     bool
	ManifestInvalid bool
	HasKeycloakDump bool
	HasGiteaDump    bool
	HasHarborDump   bool
	HasOpenBaoSnap  bool
	HasGiteaData    bool
	HasOfflineImage bool
}

func List(root string) ([]Info, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var backups []Info
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := inspect(path, entry.Name())
		if err != nil {
			return nil, err
		}
		backups = append(backups, info)
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}

func Latest(root string) (Info, bool, error) {
	backups, err := List(root)
	if err != nil {
		return Info{}, false, err
	}
	if len(backups) == 0 {
		return Info{}, false, nil
	}
	return backups[0], true, nil
}

func inspect(path, id string) (Info, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	createdAt := stat.ModTime()
	if parsed, err := time.Parse("20060102-150405", id); err == nil {
		createdAt = parsed
	}

	manifest, hasManifest, err := ReadManifest(path)
	manifestInvalid := false
	if err != nil {
		manifestInvalid = true
	} else if hasManifest && !manifest.CreatedAt.IsZero() {
		createdAt = manifest.CreatedAt
	}

	size, err := dirSize(path)
	if err != nil {
		return Info{}, err
	}

	return Info{
		ID:              id,
		Path:            path,
		CreatedAt:       createdAt,
		SizeBytes:       size,
		HasManifest:     hasManifest,
		ManifestInvalid: manifestInvalid,
		HasKeycloakDump: fileExists(filepath.Join(path, "keycloak.dump")),
		HasGiteaDump:    fileExists(filepath.Join(path, "gitea.dump")),
		HasHarborDump:   fileExists(filepath.Join(path, "harbor.dump")),
		HasOpenBaoSnap:  fileExists(filepath.Join(path, "openbao.snap")),
		HasGiteaData:    dirExists(filepath.Join(path, "gitea-data")),
		HasOfflineImage: fileExists(filepath.Join(path, "offline-images.tar")),
	}, nil
}

func dirSize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}

func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
