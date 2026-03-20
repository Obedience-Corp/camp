//go:build !dev

package main

import "testing"

func TestReleaseProfileStable_GendocsCommandHiddenButRegistered(t *testing.T) {
	assertGendocsCommand(t)
}

func TestReleaseProfileStable_DevCommandsAbsent(t *testing.T) {
	assertDevCommandsAbsent(t)
}

func TestReleaseProfileStable_BuildProfileRegistered(t *testing.T) {
	assertBuildProfileCommand(t)
}
