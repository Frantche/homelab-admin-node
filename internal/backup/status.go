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
	type policy struct {
		kind     string
		required bool
		maxAge   time.Duration
	}
	policyInputs := []struct {
		kind, key string
		required  bool
		fallback  time.Duration
	}{
		{StatusStandard, "BACKUP_STANDARD_MAX_AGE", true, 36 * time.Hour},
		{StatusGiteaProcess, "BACKUP_GITEA_PROCESS_MAX_AGE", giteaRequired, 36 * time.Hour},
		{StatusRemote, "BACKUP_REMOTE_MAX_AGE", remoteRequired, 36 * time.Hour},
		{StatusOfflineImages, "BACKUP_OFFLINE_MAX_AGE", false, 0},
		{StatusIntegrityCheck, "BACKUP_INTEGRITY_MAX_AGE", integrityRequired, 192 * time.Hour},
	}
	policies := make([]policy, 0, len(policyInputs))
	for _, input := range policyInputs {
		maxAge, err := durationValue(values[input.key], input.fallback)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", input.key, err)
		}
		policies = append(policies, policy{kind: input.kind, required: input.required, maxAge: maxAge})
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
		if (policy.kind == StatusStandard || policy.kind == StatusRemote || policy.kind == StatusOfflineImages) && !ValidID(marker.BackupID) {
			result.Present = false
			result.Message = "invalid or missing backup id"
			results = append(results, result)
			continue
		}
		result.Age = now.Sub(marker.CompletedAt)
		if result.Age < 0 {
			result.Age = 0
			result.Message = "success marker is in the future"
			results = append(results, result)
			continue
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

func durationValue(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	if value == "0" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("must be zero or a non-negative Go duration, got %q", value)
	}
	return parsed, nil
}

func FormatFreshnessAge(value time.Duration) string {
	if value < time.Minute {
		return strconv.FormatInt(int64(value.Seconds()), 10) + "s"
	}
	return value.Round(time.Minute).String()
}
