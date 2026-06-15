// Package commitkit provides a stable public API for campaign-aware git
// operations. It wraps camp's internal git and campaign packages so that
// external tools (e.g. fest) can import them without depending on internal
// implementation paths.
//
// All staging and commit operations use automatic lock retry with stale
// lock cleanup, making them resilient to index.lock contention.
//
// SyncSubmoduleRef commits only the requested submodule gitlink path and
// preserves unrelated staged campaign-root content.
package commitkit

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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

// TagComponents is the parsed form of a campaign tag. Re-exported from
// internal/git so callers do not need to import the internal package.
type TagComponents = git.TagComponents

// FormatContextTagsFull composes the consolidated campaign tag from any
// subset of (campaign id, quest id, festival ref, workitem ref). Component
// order is fixed: campaign → quest → festival → workitem.
func FormatContextTagsFull(campaignID, questID, festRef, workitemRef string) string {
	return git.FormatContextTagsFull(campaignID, questID, festRef, workitemRef)
}

// PrependContextTagsFull prepends the consolidated campaign tag to a commit
// message. If campaignID is empty, returns the message unchanged (no tag
// without a campaign).
func PrependContextTagsFull(campaignID, questID, festRef, workitemRef, message string) string {
	tag := FormatContextTagsFull(campaignID, questID, festRef, workitemRef)
	if tag == "" {
		return message
	}
	return tag + " " + message
}

// ParseTag extracts the components of a campaign tag from a commit subject.
// Returns a zero-valued TagComponents when no tag is present.
func ParseTag(subject string) TagComponents {
	return git.ParseTag(subject)
}

// TagParseWarning records a degraded parse from ParseTagDetailed.
// Re-exported from internal/git.
type TagParseWarning = git.TagParseWarning

// ParseTagDetailed is the warnings-aware peer of ParseTag. Callers that
// need to surface "tag was malformed" diagnostics (commit query output,
// doctor, etc.) should call this instead of ParseTag.
func ParseTagDetailed(subject string) (TagComponents, []TagParseWarning) {
	return git.ParseTagDetailed(subject)
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
		return "", camperrors.Wrapf(err, "commitkit: load campaign config at %s", root)
	}

	return cfg.ID, nil
}

// LoadCampaignID reads the campaign ID from the campaign.yaml located at
// campaignRoot. campaignRoot must be the directory that contains .campaign/.
func LoadCampaignID(ctx context.Context, campaignRoot string) (string, error) {
	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return "", camperrors.Wrapf(err, "commitkit: load campaign config at %s", campaignRoot)
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

// ShortHash returns the short commit hash of HEAD in the repository at repoPath.
func ShortHash(ctx context.Context, repoPath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", camperrors.Wrap(err, "commitkit: rev-parse --short HEAD")
	}
	return strings.TrimSpace(string(out)), nil
}

// SyncSubmoduleRef stages the updated submodule pointer for projectRelPath
// inside the campaign root and commits it with a campaign-tagged message.
// projectRelPath is the path to the submodule relative to campaignRoot
// (e.g. "projects/fest").
//
// It is a no-op and returns nil when the submodule pointer has not changed
// for projectRelPath.
//
// Uses automatic lock retry with stale lock cleanup.
func SyncSubmoduleRef(ctx context.Context, campaignRoot, projectRelPath, campaignID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Stage only the submodule ref — not the entire working tree.
	if err := git.StageFiles(ctx, campaignRoot, projectRelPath); err != nil {
		return camperrors.Wrapf(err, "commitkit: stage submodule %s", projectRelPath)
	}

	// Check only the ref path so unrelated staged root content is preserved.
	hasChanges, err := git.HasStagedPathChange(ctx, campaignRoot, projectRelPath)
	if err != nil {
		return camperrors.Wrapf(err, "commitkit: check staged submodule %s", projectRelPath)
	}
	if !hasChanges {
		return nil // No-op: submodule pointer hasn't changed.
	}

	msg := git.PrependCampaignTag(campaignID,
		fmt.Sprintf("sync submodule ref: %s", projectRelPath))

	if err := git.CommitScoped(ctx, campaignRoot, []string{projectRelPath}, &git.CommitOptions{Message: msg}); err != nil {
		return camperrors.Wrapf(err, "commitkit: commit submodule ref for %s", projectRelPath)
	}

	return nil
}
