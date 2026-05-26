//go:build failpoint_enabled
// +build failpoint_enabled

package failpoint

import "os"

// This file compiles ONLY when the `failpoint_enabled` build tag is set.
// Production releases must never set this tag. Test harnesses that want
// to exercise crash/kill recovery rebuild the binary with
// `-tags failpoint_enabled` before invoking the camp under test.

// runPanic terminates the goroutine with a panic so deferred recovery
// paths in production code can be exercised under test.
func runPanic(site string) error {
	panic("failpoint: " + site)
}

// runKill terminates the process immediately so kill-recovery integration
// tests can observe the on-disk state after an abrupt exit.
func runKill(site string) error {
	os.Exit(137)
	return nil // unreachable; satisfies the signature
}
