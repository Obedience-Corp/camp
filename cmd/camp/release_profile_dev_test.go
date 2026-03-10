//go:build dev

package main

import "testing"

func TestReleaseProfileDev_GendocsCommandHiddenButRegistered(t *testing.T) {
	assertGendocsCommand(t)
}

func TestReleaseProfileDev_FlowCommandRegistered(t *testing.T) {
	assertFlowCommandRegistered(t)
}

func TestReleaseProfileDev_FreshCommandRegistered(t *testing.T) {
	assertFreshCommandRegistered(t)
}
