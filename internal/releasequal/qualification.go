package releasequal

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	tagPattern    = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

type manifest struct {
	Release struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
	} `json:"release"`
}

func Verify(repoDir, name, pin, channel, qualificationPath string) error {
	switch channel {
	case "development":
		if name != "main" || (pin != "main" && pin != "refs/heads/main") {
			return fmt.Errorf("development channel must select main")
		}
		return nil
	case "ci":
		if !commitPattern.MatchString(name) || name != pin {
			return fmt.Errorf("CI channel must select one full commit SHA")
		}
		return nil
	case "production":
		if !tagPattern.MatchString(name) || !commitPattern.MatchString(pin) {
			return fmt.Errorf("production channel requires a semver tag and full commit pin")
		}
	default:
		return fmt.Errorf("unsupported release channel %q", channel)
	}

	content, err := os.ReadFile(qualificationPath)
	if err != nil {
		return fmt.Errorf("read production qualification %s: %w", qualificationPath, err)
	}
	var document manifest
	if err := json.Unmarshal(content, &document); err != nil {
		return fmt.Errorf("parse production qualification: %w", err)
	}
	if document.Release.Tag != name || document.Release.Commit != pin {
		return fmt.Errorf("production qualification does not match selected tag %s and pin %s", name, pin)
	}
	tagRef := "refs/tags/" + name
	tagType, err := exec.Command("git", "-C", repoDir, "cat-file", "-t", tagRef).Output()
	if err != nil || strings.TrimSpace(string(tagType)) != "tag" {
		return fmt.Errorf("qualified release tag %s is unavailable or not annotated", name)
	}
	resolved, err := exec.Command("git", "-C", repoDir, "rev-parse", tagRef+"^{commit}").Output()
	if err != nil || strings.TrimSpace(string(resolved)) != pin {
		return fmt.Errorf("qualified release tag %s does not resolve to pin %s", name, pin)
	}
	return nil
}
