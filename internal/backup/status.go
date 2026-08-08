package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

const (
	StatusStandard       = "standard"
	StatusOfflineImages  = "offline-images"
	StatusRemote         = "remote"
	StatusGiteaProcess   = "gitea-process"
	StatusIntegrityCheck = "integrity-check"
)

type SuccessStatus struct {
	Kind        string    `json:"kind"`
	BackupID    string    `json:"backup_id,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

type FreshnessStatus struct {
	Kind     string
	Required bool
	Present  bool
	Fresh    bool
	Age      time.Duration
	MaxAge   time.Duration
	BackupID string
	Message  string
}

func WriteSuccessStatus(root, kind, backupID string, completedAt time.Time) error {
	if root == "" || filepath.Clean(root) == "/" {
		return fmt.Errorf("backup status root must be a non-root path")
	}
	if kind == "" || filepath.Base(kind) != kind {
		return fmt.Errorf("invalid backup status kind")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create backup status directory: %w", err)
	}
	status := SuccessStatus{Kind: kind, BackupID: backupID, CompletedAt: completedAt.UTC()}
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(root, "."+kind+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create backup status marker: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Join(root, kind+".json"))
}

func CheckFreshness(now time.Time, envFile, statusRoot string) ([]FreshnessStatus, error) {
	values, err := parseEnvFile(envFile)
	if err != nil {
		return nil, err
	}
	repositories := fields(values["RESTIC_REPOSITORIES"])
	remoteRequired := parseBool(values["BACKUP_REQUIRE_REMOTE_REPOSITORY"], false)
	giteaRequired := parseBool(values["GITEA_PROCESS_BACKUP_ENABLED"], false)
	integrityRequired := len(repositories) > 0 || values["RESTIC_REPOSITORY"] != ""
	policies := []struct {
		kind     string
		required bool
		maxAge   time.Duration
	}{
		{StatusStandard, true, durationValue(values["BACKUP_STANDARD_MAX_AGE"], 36*time.Hour)},
		{StatusGiteaProcess, giteaRequired, durationValue(values["BACKUP_GITEA_PROCESS_MAX_AGE"], 36*time.Hour)},
		{StatusRemote, remoteRequired, durationValue(values["BACKUP_REMOTE_MAX_AGE"], 36*time.Hour)},
		{StatusOfflineImages, false, durationValue(values["BACKUP_OFFLINE_MAX_AGE"], 0)},
		{StatusIntegrityCheck, integrityRequired, durationValue(values["BACKUP_INTEGRITY_MAX_AGE"], 192*time.Hour)},
	}
	var results []FreshnessStatus
	for _, policy := range policies {
		required := policy.required || (policy.kind == StatusOfflineImages && policy.maxAge > 0)
		result := FreshnessStatus{Kind: policy.kind, Required: required, MaxAge: policy.maxAge}
		data, readErr := os.ReadFile(filepath.Join(statusRoot, policy.kind+".json"))
		if readErr != nil {
			if !os.IsNotExist(readErr) {
				return nil, readErr
			}
			result.Message = "no successful run recorded"
			results = append(results, result)
			continue
		}
		var marker SuccessStatus
		if err := json.Unmarshal(data, &marker); err != nil || marker.Kind != policy.kind || marker.CompletedAt.IsZero() {
			result.Message = "invalid success marker"
			results = append(results, result)
			continue
		}
		result.Present = true
		result.BackupID = marker.BackupID
		result.Age = now.Sub(marker.CompletedAt)
		if result.Age < 0 {
			result.Age = 0
		}
		result.Fresh = policy.maxAge <= 0 || result.Age <= policy.maxAge
		if result.Fresh {
			result.Message = "last success is fresh"
		} else {
			result.Message = "last success is stale"
		}
		results = append(results, result)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Kind < results[j].Kind })
	return results, nil
}

func FreshnessFailed(results []FreshnessStatus) bool {
	for _, result := range results {
		if result.Required && (!result.Present || !result.Fresh) {
			return true
		}
	}
	return false
}

func durationValue(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	if value == "0" {
		return 0
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func FormatFreshnessAge(value time.Duration) string {
	if value < time.Minute {
		return strconv.FormatInt(int64(value.Seconds()), 10) + "s"
	}
	return value.Round(time.Minute).String()
}
