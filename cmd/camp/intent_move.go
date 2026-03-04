package main

import (
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentMoveCmd = &cobra.Command{
	Use:   "move <id> <status>",
	Short: "Move intent to a different status",
	Long: `Transition an intent between lifecycle statuses.

VALID STATUSES:
  inbox    Captured, not yet reviewed
  active   Being enriched with details
  ready    Ready for Festival promotion
  done     Resolved
  killed   Abandoned

VALID TRANSITIONS:
  inbox  → active, killed
  active → ready, inbox, killed
  ready  → done, active, killed
  killed → inbox (un-kill)

Examples:
  camp intent move add-dark active        Move to active status
  camp intent move add-dark ready         Mark as ready for promotion
  camp intent move add-dark done          Mark as complete
  camp intent move add-dark killed        Archive/abandon intent`,
	Args: cobra.ExactArgs(2),
	RunE: runIntentMove,
}

func init() {
	intentCmd.AddCommand(intentMoveCmd)

	intentMoveCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
}

func runIntentMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	newStatus := args[1]
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	// Validate status
	status := intent.Status(newStatus)
	switch status {
	case intent.StatusInbox, intent.StatusActive, intent.StatusReady, intent.StatusDone, intent.StatusKilled:
		// Valid status
	default:
		return fmt.Errorf("invalid status: %s (use inbox, active, ready, done, or killed)", newStatus)
	}

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Ensure directories exist and migrate legacy layout
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring intent directories")
	}

	// Get intent title for commit message (before moving)
	i, err := svc.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("intent not found: %s", id)
	}
	intentTitle := i.Title
	sourcePath := i.Path

	// Move the intent
	result, err := svc.Move(ctx, id, status)
	if err != nil {
		return camperrors.Wrap(err, "failed to move intent")
	}

	fmt.Printf("✓ Intent moved to %s: %s\n", status, result.Path)

	// Auto-commit (unless --no-commit)
	if !noCommit {
		files := commit.NormalizeFiles(campaignRoot, sourcePath, result.Path)
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot:  campaignRoot,
				CampaignID:    cfg.ID,
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      commit.IntentMove,
			IntentTitle: intentTitle,
			Description: fmt.Sprintf("Moved to %s status", status),
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
