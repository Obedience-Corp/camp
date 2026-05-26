//go:build !failpoint_enabled
// +build !failpoint_enabled

package failpoint

// This file is the DEFAULT compile target. Production releases never set
// the `failpoint_enabled` build tag, so a leaked CAMP_TEST_FAILPOINT can
// at worst surface a failpointError; it can never panic or terminate the
// process. The unsafe peer in actions_unsafe.go is opt-in for test runs.

// runPanic in production builds downgrades the configured ActionPanic to
// an ActionError-equivalent return. A real test run rebuilds with
// `-tags failpoint_enabled` to swap in the panicking implementation.
func runPanic(site string) error {
	return failpointError{site: site + " (panic suppressed: build without -tags failpoint_enabled)"}
}

// runKill in production builds downgrades the configured ActionKill the
// same way. Process termination from an env var alone is the exact attack
// surface the build-tag boundary exists to close.
func runKill(site string) error {
	return failpointError{site: site + " (kill suppressed: build without -tags failpoint_enabled)"}
}
