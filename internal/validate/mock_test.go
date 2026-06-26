package validate

import (
	"context"
	"testing"

	"github.com/Frantche/homelab-admin-node/internal/config"
)

func TestAllMocked(t *testing.T) {
	v := Validator{Config: config.Config{ValidateMockAll: true}}
	results := v.All(context.Background())
	if len(results) == 0 {
		t.Fatal("expected validation results")
	}
	for _, result := range results {
		if result.Status != StatusSkipped {
			t.Fatalf("%s status = %s, want %s", result.Name, result.Status, StatusSkipped)
		}
	}
	if HasFailure(results) {
		t.Fatal("mocked validation should not fail")
	}
}
