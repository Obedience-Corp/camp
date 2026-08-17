//go:build !unix

package itestenv

import "os"

// Platforms without flock get a lock that reports itself unsupported, and
// Acquire turns that into a visible warning rather than a silent no-op: a
// caller that believes it holds a lock it does not have is worse off than one
// told the guarantee is missing.
func lockFileNB(*os.File) error { return errLockUnsupported }

func unlockFile(*os.File) error { return nil }
