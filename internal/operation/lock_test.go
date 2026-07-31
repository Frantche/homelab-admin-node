package operation

import (
	"path/filepath"
	"testing"
)

func TestAcquirePreventsConcurrentOperationAndAllowsReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "operation.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Acquire(path); err == nil {
		t.Fatal("second lock acquisition unexpectedly succeeded")
	}

	release()
	releaseAgain, err := Acquire(path)
	if err != nil {
		t.Fatalf("lock was not reusable after release: %v", err)
	}
	releaseAgain()
}

func TestAcquireEmptyPathIsNoop(t *testing.T) {
	release, err := Acquire("")
	if err != nil {
		t.Fatal(err)
	}
	release()
}
