//go:build dev

package main

import "testing"

func TestReleaseProfileDev_GendocsCommandHiddenButRegistered(t *testing.T) {
	assertGendocsCommand(t)
}

func TestReleaseProfileDev_DevCommandsRegistered(t *testing.T) {
	assertDevCommandsRegistered(t)
}
