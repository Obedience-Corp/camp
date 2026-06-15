//go:build dev

package main

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/version"
)

func TestReleaseProfileDev_GendocsCommandHiddenButRegistered(t *testing.T) {
	assertGendocsCommand(t)
}

func TestReleaseProfileDev_DevCommandsRegistered(t *testing.T) {
	assertDevCommandsRegistered(t)
}

func TestReleaseProfileDev_StableCommandsRegistered(t *testing.T) {
	assertStableCommandsRegistered(t)
}

func TestReleaseProfileDev_BuildProfileRegistered(t *testing.T) {
	assertBuildProfileCommand(t)
}

func TestReleaseProfileDev_VersionProfile(t *testing.T) {
	if version.Profile != "dev" {
		t.Fatalf("version.Profile = %q, want %q", version.Profile, "dev")
	}
}
