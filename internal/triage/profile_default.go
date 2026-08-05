package triage

import "context"

// ResolveProfile returns the profile a run should execute under.
//
// This is the seam the profile layer plugs into. Today it returns the built-in
// default for every campaign; the profile sequence replaces the body with
// `.campaign/triage/profile.yaml` resolution plus named built-ins, without
// changing this signature or any caller. Runs embed whatever it returns, so a
// verdict stays explainable against the policy that produced it even after the
// profile file changes.
func ResolveProfile(ctx context.Context, campaignRoot string) (ResolvedProfile, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedProfile{}, err
	}
	_ = campaignRoot
	profile := DefaultProfile()
	profile.Normalize()
	return profile, nil
}

// ResolvedProfileName is the name recorded alongside the resolved profile.
// Constant until named profiles land.
func ResolvedProfileName() string { return ProfileNameDefault }
