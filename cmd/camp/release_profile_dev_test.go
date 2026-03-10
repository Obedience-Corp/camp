//go:build dev

package main

import "testing"

func TestReleaseProfileDev_GendocsCommandHiddenButRegistered(t *testing.T) {
	assertGendocsCommand(t)
}
