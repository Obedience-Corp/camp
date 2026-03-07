//go:build dev

package main

import "testing"

func TestReleaseProfileDev_GendocsCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"gendocs"})
	if err != nil {
		t.Fatalf("expected gendocs command in dev profile: %v", err)
	}
	if cmd == nil || cmd.Name() != "gendocs" {
		t.Fatalf("expected gendocs command, got %#v", cmd)
	}
	if got := cmd.Annotations[annotationReleaseChannel]; got != releaseChannelDevOnly {
		t.Fatalf("gendocs release_channel = %q, want %q", got, releaseChannelDevOnly)
	}
}
