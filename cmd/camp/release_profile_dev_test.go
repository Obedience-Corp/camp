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

func TestReleaseProfileDev_FlowCommandHiddenButRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"flow"})
	if err != nil {
		t.Fatalf("expected flow command: %v", err)
	}
	if cmd == nil || cmd.Name() != "flow" {
		t.Fatalf("expected flow command, got %#v", cmd)
	}
	if !cmd.Hidden {
		t.Fatal("flow should be hidden from the primary help surface")
	}
}
