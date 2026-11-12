//go:build windows

package fsutil

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
)

func TestClassifyLockCreateError_AccessDeniedIsTransient(t *testing.T) {
	err := classifyLockCreateError(&fs.PathError{Op: "open", Path: "x.lock", Err: syscall.ERROR_ACCESS_DENIED})
	var pending *transientDenialError
	if !errors.As(err, &pending) {
		t.Fatalf("access denied = %v, want *transientDenialError", err)
	}
}
