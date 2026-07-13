package config

import "context"

// CommitPrefs controls campaign-aware commit behavior for camp commit paths.
// Stored under global config.json and optionally overridden per campaign in
// .campaign/settings/local.json.
type CommitPrefs struct {
	// SyncProjectRefs when true makes `camp project commit` update the
	// campaign-root submodule pointer after a project commit (same effect as
	// passing --sync). Default is false (opt-in).
	SyncProjectRefs bool `json:"sync_project_refs,omitempty"`

	// DisableCommitTags when true skips [campaign:id-…] subject prefixes on
	// camp-managed commits (root, project, worktree, refs-sync). Default is
	// false (tags enabled).
	DisableCommitTags bool `json:"disable_commit_tags,omitempty"`
}

// TagCommits reports whether campaign subject tags should be prepended.
func (p CommitPrefs) TagCommits() bool {
	return !p.DisableCommitTags
}

// IsEmpty reports whether all fields are zero (default behavior).
func (p CommitPrefs) IsEmpty() bool {
	return !p.SyncProjectRefs && !p.DisableCommitTags
}

// EffectiveCommitPrefs merges machine-global commit prefs with an optional
// campaign-local override. When local.Commit is non-nil it fully replaces the
// global commit block for that campaign.
func EffectiveCommitPrefs(ctx context.Context, campaignRoot string) CommitPrefs {
	var prefs CommitPrefs
	if g, err := LoadGlobalConfig(ctx); err == nil && g != nil {
		prefs = g.Commit
	}
	if campaignRoot == "" {
		return prefs
	}
	if loc, err := LoadLocalSettings(ctx, campaignRoot); err == nil && loc != nil && loc.Commit != nil {
		return *loc.Commit
	}
	return prefs
}
