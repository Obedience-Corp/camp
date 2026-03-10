//go:build !dev

package main

import "testing"

func TestReleaseProfileStable_GendocsCommandHiddenButRegistered(t *testing.T) {
	assertGendocsCommand(t)
}

func TestReleaseProfileStable_FlowCommandAbsent(t *testing.T) {
	assertFlowCommandAbsent(t)
}

func TestReleaseProfileStable_FreshCommandAbsent(t *testing.T) {
	assertFreshCommandAbsent(t)
}
