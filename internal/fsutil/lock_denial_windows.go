//go:build windows

package fsutil

import (
	"errors"
	"syscall"
)

// isTransientCreateDenial reports whether an exclusive create failed with
// ERROR_ACCESS_DENIED. Windows reports that, rather than ERROR_FILE_EXISTS,
// when the create races another writer's release of the same lock file (for
// example while the old file is still delete-pending). A genuine permission
// problem also lands here; waitForFileLock surfaces it once the wait expires.
func isTransientCreateDenial(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}
