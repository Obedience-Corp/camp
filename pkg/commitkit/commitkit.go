// Package commitkit provides a stable public API for campaign-aware git
// operations. It wraps camp's internal git and campaign packages so that
// external tools (e.g. fest) can import them without depending on internal
// implementation paths.
package commitkit

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/git"
)

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

// SyncSubmoduleRef stages the updated submodule pointer for projectRelPath
// inside the campaign root and commits it with a campaign-tagged message.
// projectRelPath is the path to the submodule relative to campaignRoot
// (e.g. "projects/fest").
//
// It is a no-op and returns nil when the submodule pointer has not changed
// (git reports nothing to commit).
func SyncSubmoduleRef(ctx context.Context, campaignRoot, projectRelPath, campaignID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Stage only the submodule ref — not the entire working tree.
	stageCmd := exec.CommandContext(ctx, "git", "-C", campaignRoot, "add", projectRelPath)
	if out, err := stageCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commitkit: stage submodule %s: %s: %w",
			projectRelPath, strings.TrimSpace(string(out)), err)
	}

	// Check whether there is actually something staged before committing.
	diffCmd := exec.CommandContext(ctx, "git", "-C", campaignRoot, "diff", "--cached", "--quiet")
	if err := diffCmd.Run(); err == nil {
		// Exit 0 means no staged changes — nothing to commit.
		return nil
	}

	msg := git.PrependCampaignTag(campaignID,
		fmt.Sprintf("sync submodule ref: %s", projectRelPath))

	commitCmd := exec.CommandContext(ctx, "git", "-C", campaignRoot, "commit", "-m", msg)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commitkit: commit submodule ref for %s: %s: %w",
			projectRelPath, strings.TrimSpace(string(out)), err)
	}

	return nil
}
