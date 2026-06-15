//go:build !dev

package main

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/version"
)

func TestReleaseProfileStable_GendocsCommandHiddenButRegistered(t *testing.T) {
	assertGendocsCommand(t)
}

func TestReleaseProfileStable_DevCommandsAbsent(t *testing.T) {
	assertDevCommandsAbsent(t)
}

func TestReleaseProfileStable_StableCommandsRegistered(t *testing.T) {
	assertStableCommandsRegistered(t)
}

func TestReleaseProfileStable_BuildProfileRegistered(t *testing.T) {
	assertBuildProfileCommand(t)
}

func TestReleaseProfileStable_VersionProfile(t *testing.T) {
	if version.Profile != "stable" {
		t.Fatalf("version.Profile = %q, want %q", version.Profile, "stable")
	}
}
