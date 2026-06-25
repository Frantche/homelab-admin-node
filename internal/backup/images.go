package backup

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func DetectImages(ctx context.Context, adminRoot string) ([]string, error) {
	stackRoot := filepath.Join(adminRoot, "stacks")
	composeFiles, err := filepath.Glob(filepath.Join(stackRoot, "*", "compose.yaml"))
	if err != nil {
		return nil, err
	}
	imageSet := map[string]bool{}
	for _, composeFile := range composeFiles {
		for _, image := range composeImages(ctx, composeFile) {
			imageSet[image] = true
		}
	}
	var images []string
	for image := range imageSet {
		images = append(images, image)
	}
	sort.Strings(images)
	return images, nil
}

func composeImages(ctx context.Context, composeFile string) []string {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "config", "--images")
	output, err := cmd.Output()
	if err == nil {
		return nonEmptyLines(string(output))
	}
	return parseComposeImages(composeFile)
}

func parseComposeImages(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var images []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "image:") {
			continue
		}
		image := strings.TrimSpace(strings.TrimPrefix(line, "image:"))
		image = strings.Trim(image, `"'`)
		if image != "" {
			images = append(images, image)
		}
	}
	return images
}

func nonEmptyLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
