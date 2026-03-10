//go:build !dev

package main

import "testing"

func TestReleaseProfileStable_GendocsCommandHiddenButRegistered(t *testing.T) {
	assertGendocsCommand(t)
}
