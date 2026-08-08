package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateManifestArtifactsRejectsMissingRequiredArtifact(t *testing.T) {
	root := t.TempDir()
	manifest := Manifest{Artifacts: []ManifestArtifact{{Path: "openbao.snap", Required: true, Status: ArtifactProduced}}}
	if err := validateManifestArtifacts(manifest, root); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v, want missing artifact failure", err)
	}
}

func TestValidateManifestArtifactsAcceptsExplicitDisabledOptionalArtifact(t *testing.T) {
	root := t.TempDir()
	manifest := Manifest{Artifacts: []ManifestArtifact{{Path: "offline-images.tar", Status: ArtifactDisabled}}}
	if err := validateManifestArtifacts(manifest, root); err != nil {
		t.Fatal(err)
	}
}

func TestValidateManifestArtifactsRejectsRequiredDisabledArtifact(t *testing.T) {
	root := t.TempDir()
	manifest := Manifest{Artifacts: []ManifestArtifact{{Path: "gitea.dump", Required: true, Status: ArtifactDisabled}}}
	if err := validateManifestArtifacts(manifest, root); err == nil || !strings.Contains(err.Error(), "required artifact is disabled") {
		t.Fatalf("error = %v, want required disabled artifact failure", err)
	}
}

func TestValidateManifestArtifactsAcceptsProducedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "gitea-stack"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Artifacts: []ManifestArtifact{{Path: "gitea-stack", Required: true, Status: ArtifactProduced}}}
	if err := validateManifestArtifacts(manifest, root); err != nil {
		t.Fatal(err)
	}
}
