package backup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

type OfflineStatus struct {
	ID                  string
	CreatedAt           time.Time
	Age                 time.Duration
	Fresh               bool
	Verified            bool
	RecoveryKitComplete bool
	RecoveryKitChecked  time.Time
	Problems            []string
}

type recoveryKitInventory struct {
	LastVerifiedAt             time.Time `json:"last_verified_at"`
	AgeIdentityOffsite         bool      `json:"age_identity_offsite"`
	ConfigRepoAccessTested     bool      `json:"config_repo_access_tested"`
	ResticAccessOffsite        bool      `json:"restic_access_offsite"`
	OpenBaoRecoveryOffsite     bool      `json:"openbao_recovery_offsite"`
	SeparationOfDutiesVerified bool      `json:"separation_of_duties_verified"`
}

func CheckOfflineStatus(cfg config.Config, now time.Time) (OfflineStatus, error) {
	backups, err := List(cfg.BackupRoot)
	if err != nil {
		return OfflineStatus{}, err
	}
	var latest Info
	found := false
	for _, item := range backups {
		if item.HasOfflineImage {
			latest, found = item, true
			break
		}
	}
	status := OfflineStatus{}
	if !found {
		status.Problems = append(status.Problems, "no offline-capable recovery point exists")
	} else {
		status.ID = latest.ID
		status.CreatedAt = latest.CreatedAt
		status.Age = now.Sub(latest.CreatedAt)
		status.Fresh = status.Age >= 0 && status.Age <= cfg.OfflineBackupMaxAge
		if !status.Fresh {
			status.Problems = append(status.Problems, fmt.Sprintf("newest offline recovery point is older than %s", cfg.OfflineBackupMaxAge))
		}
		manifest, err := Verify(latest.Path)
		if err != nil {
			status.Problems = append(status.Problems, "offline recovery point verification failed: "+err.Error())
		} else if err := validateDeliveredOfflineRecoveryManifest(manifest, latest.Path); err != nil {
			status.Problems = append(status.Problems, "offline recovery point is incomplete: "+err.Error())
		} else {
			status.Verified = true
		}
	}
	checkRecoveryKit(cfg, now, &status)
	return status, nil
}

func validateScheduledOfflineRecoveryManifest(manifest Manifest, dir string) error {
	if !manifest.OfflineImages {
		return fmt.Errorf("manifest does not declare offline images")
	}
	if !manifest.RepositoryBundle || strings.TrimSpace(manifest.CLIRevision) == "" || !fileExists(filepath.Join(dir, "repository.bundle")) {
		return fmt.Errorf("repository bundle or revision is missing")
	}
	if !manifest.StackDefinitions || !dirExists(filepath.Join(dir, "stack-definitions")) {
		return fmt.Errorf("rendered stack definitions are missing")
	}
	return nil
}

func validateDeliveredOfflineRecoveryManifest(manifest Manifest, dir string) error {
	if err := validateScheduledOfflineRecoveryManifest(manifest, dir); err != nil {
		return err
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == "remote-delivery" && artifact.Required && artifact.External && artifact.Status == ArtifactProduced {
			return nil
		}
	}
	return fmt.Errorf("verified remote delivery evidence is missing")
}

func checkRecoveryKit(cfg config.Config, now time.Time, status *OfflineStatus) {
	data, err := os.ReadFile(cfg.RecoveryKitInventoryFile)
	if err != nil {
		status.Problems = append(status.Problems, "recovery-kit inventory is missing: "+cfg.RecoveryKitInventoryFile)
		return
	}
	var inventory recoveryKitInventory
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&inventory); err != nil {
		status.Problems = append(status.Problems, "recovery-kit inventory is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		status.Problems = append(status.Problems, "recovery-kit inventory is invalid")
		return
	}
	status.RecoveryKitChecked = inventory.LastVerifiedAt
	checks := []struct {
		ok   bool
		name string
	}{
		{inventory.AgeIdentityOffsite, "age identity off-site copy"},
		{inventory.ConfigRepoAccessTested, "private config-repository access"},
		{inventory.ResticAccessOffsite, "Restic credentials off-site copy"},
		{inventory.OpenBaoRecoveryOffsite, "OpenBao recovery material off-site copy"},
		{inventory.SeparationOfDutiesVerified, "separation of duties"},
	}
	complete := !inventory.LastVerifiedAt.IsZero() && now.Sub(inventory.LastVerifiedAt) >= 0 && now.Sub(inventory.LastVerifiedAt) <= cfg.RecoveryKitMaxAge
	if !complete {
		status.Problems = append(status.Problems, fmt.Sprintf("recovery-kit inventory verification is missing or older than %s", cfg.RecoveryKitMaxAge))
	}
	for _, check := range checks {
		if !check.ok {
			complete = false
			status.Problems = append(status.Problems, "recovery-kit prerequisite not attested: "+check.name)
		}
	}
	if !ageIdentityPresent(cfg.AgeKeyFile) {
		complete = false
		status.Problems = append(status.Problems, "local recovery prerequisite missing or invalid: local age identity")
	}
	if _, err := os.Stat(filepath.Join(cfg.ConfigRepoRoot, ".git")); err != nil {
		complete = false
		status.Problems = append(status.Problems, "local recovery prerequisite missing: private config repository")
	}
	if !sopsEncryptedMaterialPresent(cfg.OpenBaoRecoveryFile) {
		complete = false
		status.Problems = append(status.Problems, "local recovery prerequisite missing or invalid: SOPS-encrypted OpenBao recovery material")
	}
	if !resticOffsiteAccessDeclared(cfg.BackupEnvFile) {
		complete = false
		status.Problems = append(status.Problems, "a non-local Restic repository/password declaration is missing")
	}
	status.RecoveryKitComplete = complete
}

func ageIdentityPresent(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") && len(line) > len("AGE-SECRET-KEY-") {
			return true
		}
	}
	return false
}

func sopsEncryptedMaterialPresent(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	hasEncryptedValue := false
	hasSOPSMetadata := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		hasEncryptedValue = hasEncryptedValue || strings.Contains(line, "ENC[")
		hasSOPSMetadata = hasSOPSMetadata || strings.HasPrefix(line, "sops:")
	}
	return hasEncryptedValue && hasSOPSMetadata
}

func resticOffsiteAccessDeclared(path string) bool {
	cfg, err := loadResticConfig(path)
	if err != nil {
		return false
	}
	if len(cfg.Repositories) > 0 {
		for _, id := range cfg.Repositories {
			suffix := sanitizeRepoID(id)
			values := cfg.RepoValues[suffix]
			repository := values["RESTIC_REPOSITORY"]
			if isRemoteRepository(repository) && values["RESTIC_PASSWORD"] != "" && validateSecureRepository(repository, cfg.RequireSecureRepos) == nil {
				return true
			}
		}
		return false
	}
	return isRemoteRepository(cfg.Repository) && cfg.Password != "" && validateSecureRepository(cfg.Repository, cfg.RequireSecureRepos) == nil
}
