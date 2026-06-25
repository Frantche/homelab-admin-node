package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectImagesFromComposeFallback(t *testing.T) {
	adminRoot := t.TempDir()
	stackDir := filepath.Join(adminRoot, "stacks/gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte(`services:
  gitea:
    image: docker.gitea.com/gitea:1.26.4
  db:
    image: "pgautoupgrade/pgautoupgrade:18-trixie"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	images, err := DetectImages(context.Background(), adminRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 {
		t.Fatalf("images = %#v", images)
	}
	if images[0] != "docker.gitea.com/gitea:1.26.4" || images[1] != "pgautoupgrade/pgautoupgrade:18-trixie" {
		t.Fatalf("images = %#v", images)
	}
}
