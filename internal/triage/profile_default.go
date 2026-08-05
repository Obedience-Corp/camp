package triage

import "context"

// ResolveProfile returns the profile a run should execute under: the
// campaign's `.campaign/triage/profile.yaml` merged over the built-in default,
// or the built-in default alone when the campaign has no file.
//
// It delegates to ResolveProfileNamed and exists for callers that want the
// campaign's profile and have no run-specific name to pass. Runs embed
// whatever it returns, so a verdict stays explainable against the policy that
// produced it even after the profile file changes.
func ResolveProfile(ctx context.Context, campaignRoot string) (ResolvedProfile, error) {
	resolution, err := ResolveProfileNamed(ctx, campaignRoot, "")
	if err != nil {
		return ResolvedProfile{}, err
	}
	return resolution.Profile, nil
}

// ResolvedProfileName is the name recorded alongside the resolved profile.
// Constant until named profiles land.
func ResolvedProfileName() string { return ProfileNameDefault }
