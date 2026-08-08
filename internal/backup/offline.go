package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		if _, err := Verify(latest.Path); err != nil {
			status.Problems = append(status.Problems, "offline recovery point verification failed: "+err.Error())
		} else {
			status.Verified = true
		}
	}
	checkRecoveryKit(cfg, now, &status)
	return status, nil
}

func checkRecoveryKit(cfg config.Config, now time.Time, status *OfflineStatus) {
	data, err := os.ReadFile(cfg.RecoveryKitInventoryFile)
	if err != nil {
		status.Problems = append(status.Problems, "recovery-kit inventory is missing: "+cfg.RecoveryKitInventoryFile)
		return
	}
	var inventory recoveryKitInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
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
	for _, prerequisite := range []struct {
		path string
		name string
	}{
		{cfg.AgeKeyFile, "local age identity"},
		{filepath.Join(cfg.ConfigRepoRoot, ".git"), "private config repository"},
		{cfg.OpenBaoRecoveryFile, "encrypted OpenBao recovery material"},
	} {
		if _, err := os.Stat(prerequisite.path); err != nil {
			complete = false
			status.Problems = append(status.Problems, "local recovery prerequisite missing: "+prerequisite.name)
		}
	}
	if !resticRepositoryAccessDeclared(cfg.BackupEnvFile) {
		complete = false
		status.Problems = append(status.Problems, "Restic repository/password declarations are missing")
	}
	status.RecoveryKitComplete = complete
}

func resticRepositoryAccessDeclared(path string) bool {
	values, err := parseEnvFile(path)
	if err != nil {
		return false
	}
	if values["RESTIC_REPOSITORY"] != "" && values["RESTIC_PASSWORD"] != "" {
		return true
	}
	for _, id := range fields(values["RESTIC_REPOSITORIES"]) {
		suffix := sanitizeRepoID(id)
		if values["RESTIC_REPOSITORY_"+suffix] != "" && values["RESTIC_PASSWORD_"+suffix] != "" {
			return true
		}
	}
	return false
}
