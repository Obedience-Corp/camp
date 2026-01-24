package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/paths"
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
}

func runIntentArchive(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Archive the intent (uses Archive method which calls Move with StatusKilled)
	result, err := svc.Archive(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to archive intent: %w", err)
	}

	fmt.Printf("✓ Intent archived: %s\n", result.Path)
	return nil
}
