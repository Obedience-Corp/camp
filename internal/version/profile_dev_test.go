//go:build dev

package version

import "testing"

func TestProfileDev(t *testing.T) {
	if Profile != "dev" {
		t.Fatalf("Profile = %q, want %q", Profile, "dev")
	}
	if Get().Profile != "dev" {
		t.Fatalf("Get().Profile = %q, want %q", Get().Profile, "dev")
	}
}
