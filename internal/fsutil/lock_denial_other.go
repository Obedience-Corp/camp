//go:build !windows

package fsutil

// isTransientCreateDenial is always false off Windows: unlink removes the name
// immediately, so an exclusive create never observes a release in flight.
func isTransientCreateDenial(error) bool { return false }
