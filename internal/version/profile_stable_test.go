//go:build !dev

package version

import "testing"

func TestProfileStable(t *testing.T) {
	if Profile != "stable" {
		t.Fatalf("Profile = %q, want %q", Profile, "stable")
	}
	if Get().Profile != "stable" {
		t.Fatalf("Get().Profile = %q, want %q", Get().Profile, "stable")
	}
}
