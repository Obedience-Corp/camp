package config

import (
	"context"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

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

// MergeCommitPrefs resolves effective commit prefs from an already-loaded global
// commit block and optional campaign-local settings. When local.Commit is
// non-nil it fully replaces the global block for that campaign. Use this when
// the caller already holds a consistent config snapshot; EffectiveCommitPrefs is
// the disk-loading wrapper around it.
func MergeCommitPrefs(global CommitPrefs, local *LocalSettings) CommitPrefs {
	if local != nil && local.Commit != nil {
		return *local.Commit
	}
	return global
}

// EffectiveCommitPrefs merges machine-global commit prefs with an optional
// campaign-local override. A missing global config or local settings file is not
// an error (both loaders return defaults), but a malformed or unreadable file is
// propagated so callers never silently apply a different commit policy — in
// particular so a corrupt local.json cannot inherit a global SyncProjectRefs and
// turn a project commit into a campaign-root pointer commit. On error it returns
// fail-closed zero-value prefs (no ref sync, tags on) alongside the error so any
// caller that chooses to degrade instead of aborting still fails safe.
func EffectiveCommitPrefs(ctx context.Context, campaignRoot string) (CommitPrefs, error) {
	g, err := LoadGlobalConfig(ctx)
	if err != nil {
		return CommitPrefs{}, camperrors.Wrap(err, "resolve global commit prefs")
	}
	var global CommitPrefs
	if g != nil {
		global = g.Commit
	}
	if campaignRoot == "" {
		return global, nil
	}
	local, err := LoadLocalSettings(ctx, campaignRoot)
	if err != nil {
		return CommitPrefs{}, camperrors.Wrapf(err, "resolve local commit prefs for %s", campaignRoot)
	}
	return MergeCommitPrefs(global, local), nil
}
