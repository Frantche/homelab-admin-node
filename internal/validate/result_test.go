package validate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestHasFailure(t *testing.T) {
	results := []CheckResult{{Name: "A", Status: StatusOK}, {Name: "B", Status: StatusFail}}
	if !HasFailure(results) {
		t.Fatal("HasFailure = false, want true")
	}
}

func TestWriteText(t *testing.T) {
	var out bytes.Buffer
	WriteText(&out, []CheckResult{{Name: "Keycloak", Status: StatusOK, Message: "ready"}})
	if !strings.Contains(out.String(), "KEYCLOAK") && !strings.Contains(out.String(), "Keycloak") {
		t.Fatalf("output missing check name: %q", out.String())
	}
	if !strings.Contains(out.String(), "OK") {
		t.Fatalf("output missing status: %q", out.String())
	}
}

func TestWriteJSON(t *testing.T) {
	var out bytes.Buffer
	if err := WriteJSON(&out, []CheckResult{{Name: "Keycloak", Status: StatusOK, Message: "ready"}}); err != nil {
		t.Fatal(err)
	}
	var decoded []CheckResult
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded[0].Name != "Keycloak" {
		t.Fatalf("decoded = %#v", decoded)
	}
}
