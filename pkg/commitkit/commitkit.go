// Package commitkit provides a stable public API for campaign-aware git
// operations. It wraps camp's internal git and campaign packages so that
// external tools (e.g. fest) can import them without depending on internal
// implementation paths.
//
// All staging and commit operations use automatic lock retry with stale
// lock cleanup, making them resilient to index.lock contention.
package commitkit

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git"
)

// ErrNoChanges is returned when there are no changes to commit.
var ErrNoChanges = git.ErrNoChanges

// CommitOptions configures a commit operation.
type CommitOptions struct {
	Message    string
	Amend      bool
	AllowEmpty bool
	Author     string // Optional: "Name <email>"
}

// FormatCampaignTag returns the "[OBEY-CAMPAIGN-{id}]" prefix string.
// Truncates campaignID to 8 characters. Returns empty string if campaignID
// is empty.
func FormatCampaignTag(campaignID string) string {
	return git.FormatCampaignTag(campaignID)
}

// PrependCampaignTag prepends the campaign tag to a commit message.
// If campaignID is empty, returns the message unchanged.
func PrependCampaignTag(campaignID, message string) string {
	return git.PrependCampaignTag(campaignID, message)
}

// DetectCampaign finds the campaign root by walking up from the current
// working directory. Returns the campaign ID string from the campaign's
// config, or an error if the working directory is not inside a campaign.
func DetectCampaign(ctx context.Context) (string, error) {
	root, err := campaign.DetectFromCwd(ctx)
	if err != nil {
		return "", err
	}

	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return "", fmt.Errorf("commitkit: load campaign config at %s: %w", root, err)
	}

	return cfg.ID, nil
}

// LoadCampaignID reads the campaign ID from the campaign.yaml located at
// campaignRoot. campaignRoot must be the directory that contains .campaign/.
func LoadCampaignID(ctx context.Context, campaignRoot string) (string, error) {
	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return "", fmt.Errorf("commitkit: load campaign config at %s: %w", campaignRoot, err)
	}

	return cfg.ID, nil
}

// StageAll stages all changes in the repository at repoPath.
// Uses automatic lock retry with stale lock cleanup.
func StageAll(ctx context.Context, repoPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return git.StageAll(ctx, repoPath)
}

// StageFiles stages specific files in the repository at repoPath.
// Uses automatic lock retry with stale lock cleanup.
func StageFiles(ctx context.Context, repoPath string, files ...string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return git.StageFiles(ctx, repoPath, files...)
}

// HasStagedChanges reports whether there are staged changes ready to commit
// in the repository at repoPath.
func HasStagedChanges(ctx context.Context, repoPath string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return git.HasStagedChanges(ctx, repoPath)
}

// Commit creates a git commit in the repository at repoPath.
// Uses automatic lock retry with stale lock cleanup.
// Returns ErrNoChanges if there is nothing to commit.
func Commit(ctx context.Context, repoPath string, opts CommitOptions) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return git.Commit(ctx, repoPath, &git.CommitOptions{
		Message:    opts.Message,
		Amend:      opts.Amend,
		AllowEmpty: opts.AllowEmpty,
		Author:     opts.Author,
	})
}

// CommitAll stages all changes and commits them with the given message.
// Returns ErrNoChanges if there is nothing to commit.
// Uses automatic lock retry with stale lock cleanup.
func CommitAll(ctx context.Context, repoPath, message string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return git.CommitAll(ctx, repoPath, message)
}

// StageAllExcludingSubmodules stages all changes but excludes submodule ref
// updates. Reads submodule paths from .gitmodules and unstages them after
// a broad stage. Use this instead of StageAll when committing at a campaign
// root to prevent submodule refs from polluting commits.
func StageAllExcludingSubmodules(ctx context.Context, repoPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return git.StageAllExcludingSubmodules(ctx, repoPath)
}

// ShortHash returns the short commit hash of HEAD in the repository at repoPath.
func ShortHash(ctx context.Context, repoPath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("commitkit: rev-parse --short HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// SyncSubmoduleRef stages the updated submodule pointer for projectRelPath
// inside the campaign root and commits it with a campaign-tagged message.
// projectRelPath is the path to the submodule relative to campaignRoot
// (e.g. "projects/fest").
//
// It is a no-op and returns nil when the submodule pointer has not changed
// (git reports nothing to commit).
//
// Uses automatic lock retry with stale lock cleanup.
func SyncSubmoduleRef(ctx context.Context, campaignRoot, projectRelPath, campaignID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Stage only the submodule ref — not the entire working tree.
	if err := git.StageFiles(ctx, campaignRoot, projectRelPath); err != nil {
		return fmt.Errorf("commitkit: stage submodule %s: %w", projectRelPath, err)
	}

	// Check whether there is actually something staged before committing.
	hasChanges, err := git.HasStagedChanges(ctx, campaignRoot)
	if err != nil {
		return fmt.Errorf("commitkit: check staged changes: %w", err)
	}
	if !hasChanges {
		return nil // No-op: submodule pointer hasn't changed.
	}

	msg := git.PrependCampaignTag(campaignID,
		fmt.Sprintf("sync submodule ref: %s", projectRelPath))

	if err := git.Commit(ctx, campaignRoot, &git.CommitOptions{Message: msg}); err != nil {
		return fmt.Errorf("commitkit: commit submodule ref for %s: %w", projectRelPath, err)
	}

	return nil
}
