package operation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const lockRetryInterval = 100 * time.Millisecond

func Acquire(path string) (func(), error) {
	return acquire(path, false)
}

// AcquireWait waits up to timeout for the operation lock. A non-positive
// timeout preserves the non-blocking Acquire behavior.
func AcquireWait(ctx context.Context, path string, timeout time.Duration) (func(), error) {
	if timeout <= 0 {
		return Acquire(path)
	}
	if path == "" {
		return func() {}, nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		unlock, err := acquire(path, true)
		if err == nil {
			return unlock, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, err
		}

		timer := time.NewTimer(lockRetryInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return nil, fmt.Errorf("waiting for admin-node operation lock: %w", ctx.Err())
			}
			return nil, fmt.Errorf("timed out after %s waiting for admin-node operation lock", timeout)
		case <-timer.C:
		}
	}
}

func acquire(path string, exposeContention bool) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if exposeContention && (errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)) {
			return nil, err
		}
		return nil, fmt.Errorf("another admin-node operation is already running: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}
