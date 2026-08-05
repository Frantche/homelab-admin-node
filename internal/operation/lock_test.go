package operation

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestAcquireWaitSucceedsAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		release()
	}()

	releaseAgain, err := AcquireWait(context.Background(), path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	releaseAgain()
}

func TestAcquireWaitTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = AcquireWait(context.Background(), path, 150*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("AcquireWait() error = %v, want timeout", err)
	}
}

func TestAcquireWaitHonorsContextCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = AcquireWait(ctx, path, time.Second)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("AcquireWait() error = %v, want context cancellation", err)
	}
}

func TestAcquireEmptyPathIsNoop(t *testing.T) {
	release, err := Acquire("")
	if err != nil {
		t.Fatal(err)
	}
	release()
}
