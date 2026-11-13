package fsutil

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

var errDenialCause = errors.New("access is denied")

func TestWaitForFileLock_RetriesTransientDenial(t *testing.T) {
	attempts := 0
	release, err := waitForFileLock(context.Background(), "project-link.lock", lockLivenessBudget, func() (func(), bool, error) {
		attempts++
		if attempts < 3 {
			return nil, false, &transientDenialError{err: errDenialCause}
		}
		return func() {}, true, nil
	})
	if err != nil {
		t.Fatalf("waitForFileLock error = %v, want acquired after a transient denial", err)
	}
	if release == nil || attempts != 3 {
		t.Fatalf("release=%v attempts=%d, want acquired on attempt 3", release != nil, attempts)
	}
}

func TestWaitForFileLock_PersistentDenialReportsCause(t *testing.T) {
	_, err := waitForFileLock(context.Background(), "project-link.lock", time.Millisecond, func() (func(), bool, error) {
		return nil, false, &transientDenialError{err: errDenialCause}
	})
	if !errors.Is(err, errDenialCause) {
		t.Fatalf("waitForFileLock error = %v, want underlying cause", err)
	}
	if errors.Is(err, camperrors.ErrTimeout) {
		t.Fatalf("persistent denial must not be reported as a holder timeout: %v", err)
	}
}

func TestWaitForFileLock_HeldAfterDenialReportsTimeout(t *testing.T) {
	attempts := 0
	_, err := waitForFileLock(context.Background(), "project-link.lock", time.Millisecond, func() (func(), bool, error) {
		attempts++
		if attempts == 1 {
			return nil, false, &transientDenialError{err: errDenialCause}
		}
		time.Sleep(2 * time.Millisecond)
		return nil, false, nil
	})
	if !errors.Is(err, camperrors.ErrTimeout) {
		t.Fatalf("waitForFileLock error = %v, want ErrTimeout once the lock is plainly held", err)
	}
}

func TestClassifyLockCreateError(t *testing.T) {
	if err := classifyLockCreateError(&fs.PathError{Op: "open", Path: "x.lock", Err: fs.ErrExist}); err != nil {
		t.Fatalf("existing lock = %v, want nil (held)", err)
	}
	hard := classifyLockCreateError(&fs.PathError{Op: "open", Path: "x.lock", Err: fs.ErrNotExist})
	var pending *transientDenialError
	if hard == nil || errors.As(hard, &pending) || !errors.Is(hard, fs.ErrNotExist) {
		t.Fatalf("missing parent = %v, want wrapped hard error", hard)
	}
}
