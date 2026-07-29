package defercommit

import (
	"context"
	"testing"
)

// Each refusal below is a correctness rule, not a preference. A copy of this
// decision that drifted by one condition would silently either skip a user's
// git hook or break the synchronous contract --json consumers depend on, so
// every reason gets its own test naming what it protects.

func TestAllowedRefusals(t *testing.T) {
	// A path inside a campaign that resolves to a lane. Hook detection is the
	// only condition that shells out to git, and it is checked last, so these
	// cases never reach it.
	const campaignRoot = "/campaigns/demo"
	const repoPath = "/campaigns/demo/projects/camp"

	tests := []struct {
		name string
		env  string
		req  Request
		want Refusal
	}{
		{
			name: "CAMP_NO_DEFER turns deferral off entirely",
			env:  "1",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: repoPath},
			want: RefusedDisabled,
		},
		{
			name: "a non-zero value other than 1 still means off",
			env:  "false",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: repoPath},
			want: RefusedDisabled,
		},
		{
			name: "--json is synchronous by contract",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: repoPath, JSON: true},
			want: RefusedJSON,
		},
		{
			name: "--amend rewrites HEAD so the parent check cannot apply",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: repoPath, Amend: true},
			want: RefusedAmend,
		},
		{
			name: "outside a campaign there is no queue",
			req:  Request{CampaignRoot: "", RepoPath: repoPath},
			want: RefusedNoCampaign,
		},
		{
			name: "a repo outside the campaign tree has no lane",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: "/somewhere/else"},
			want: RefusedNoCampaign,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvNoDefer, tt.env)
			allowed, why := Allowed(context.Background(), tt.req)
			if allowed {
				t.Fatalf("deferral was allowed; want refusal %q", tt.want)
			}
			if why != tt.want {
				t.Errorf("refusal = %q, want %q", why, tt.want)
			}
		})
	}
}

// "0" and empty are the only values that leave deferral on. Someone exporting
// CAMP_NO_DEFER=false is asking for it off, and guessing the other way would
// defer for a user who explicitly said not to.
func TestDisabled(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", false},
		{"1", true},
		{"true", true},
		{"false", true},
		{" 1 ", true},
	}
	for _, tt := range tests {
		t.Run("value="+tt.value, func(t *testing.T) {
			t.Setenv(EnvNoDefer, tt.value)
			if got := Disabled(); got != tt.want {
				t.Errorf("Disabled() with %q = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// Refusals are ordered so a user hears about their own configuration before
// camp's internal constraints, and so the only condition that shells out to
// git runs last.
func TestRefusalOrderPutsTheUsersOwnSwitchFirst(t *testing.T) {
	t.Setenv(EnvNoDefer, "1")
	_, why := Allowed(context.Background(), Request{
		CampaignRoot: "/campaigns/demo",
		RepoPath:     "/campaigns/demo/projects/camp",
		JSON:         true,
		Amend:        true,
	})
	if why != RefusedDisabled {
		t.Errorf("refusal = %q; the user's own switch must be reported first", why)
	}
}
