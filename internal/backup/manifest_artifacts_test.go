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

func TestValidateManifestArtifactsRejectsFailedArtifactInCompleteManifest(t *testing.T) {
	manifest := Manifest{Complete: true, Artifacts: []ManifestArtifact{{Path: "openbao.snap", Required: true, Status: ArtifactFailed}}}
	if err := validateManifestArtifacts(manifest, t.TempDir()); err == nil || !strings.Contains(err.Error(), "complete manifest") {
		t.Fatalf("error = %v, want complete manifest failure", err)
	}
}

func TestValidateManifestArtifactsAcceptsExternalDelivery(t *testing.T) {
	manifest := Manifest{Complete: true, Artifacts: []ManifestArtifact{{Path: "remote-delivery", Required: true, Status: ArtifactProduced, External: true}}}
	if err := validateManifestArtifacts(manifest, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
