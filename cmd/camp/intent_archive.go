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

var intentArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Archive an intent",
	Long: `Archive an intent by moving it to the killed status.

This is a convenience command equivalent to:
  camp intent move <id> killed

Archived intents are retained but hidden from default listings.
Use 'camp intent move <id> inbox' to un-archive if needed.

Examples:
  camp intent archive add-dark           Archive by partial ID
  camp intent archive 20260119-153412    Archive by full ID`,
	Args: cobra.ExactArgs(1),
	RunE: runIntentArchive,
}

func init() {
	intentCmd.AddCommand(intentArchiveCmd)

	intentArchiveCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
}

func runIntentArchive(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Get intent title for commit message (before archiving)
	i, err := svc.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("intent not found: %s", id)
	}
	intentTitle := i.Title

	// Archive the intent (uses Archive method which calls Move with StatusKilled)
	result, err := svc.Archive(ctx, id)
	if err != nil {
		return camperrors.Wrap(err, "failed to archive intent")
	}

	fmt.Printf("✓ Intent archived: %s\n", result.Path)

	// Auto-commit (unless --no-commit)
	if !noCommit {
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot: campaignRoot,
				CampaignID:   cfg.ID,
			},
			Action:      commit.IntentArchive,
			IntentTitle: intentTitle,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
