package main

import (
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Archive an intent",
	Long: `Archive an intent by moving it to dungeon/archived.

This is a convenience command equivalent to:
  camp intent move <id> archived --reason "..."

Dungeon moves require a reason and append a decision record to the intent body.
Use 'camp intent move <id> inbox' to un-archive if needed.

Examples:
  camp intent archive add-dark --reason "superseded by broader initiative"
  camp intent archive 20260119-153412 --reason "preserve as reference"`,
	Args: cobra.ExactArgs(1),
	RunE: runIntentArchive,
}

func init() {
	intentCmd.AddCommand(intentArchiveCmd)

	intentArchiveCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
	intentArchiveCmd.Flags().String("reason", "", "Reason for archiving (required)")
}

func runIntentArchive(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	noCommit, _ := cmd.Flags().GetBool("no-commit")
	reason, _ := cmd.Flags().GetString("reason")
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("--reason is required when archiving an intent")
	}

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring intent directories")
	}

	// Get intent title for commit message (before archiving)
	i, err := svc.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("intent not found: %s", id)
	}
	intentTitle := i.Title
	sourcePath := i.Path
	prevStatus := i.Status

	if prevStatus == intent.StatusArchived {
		fmt.Printf("Intent already archived: %s\n", i.Path)
		return nil
	}

	intent.AppendDecisionRecord(i, intent.StatusArchived, reason)
	if err := svc.Save(ctx, i); err != nil {
		return camperrors.Wrap(err, "failed to save decision record")
	}

	// Archive the intent (moves to dungeon/archived)
	result, err := svc.Move(ctx, id, intent.StatusArchived)
	if err != nil {
		return camperrors.Wrap(err, "failed to archive intent")
	}

	if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
		Type:   audit.EventArchive,
		ID:     i.ID,
		Title:  intentTitle,
		From:   string(prevStatus),
		To:     string(intent.StatusArchived),
		Reason: reason,
	}); err != nil {
		return err
	}

	fmt.Printf("✓ Intent archived: %s\n", result.Path)

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
			Action:      commit.IntentArchive,
			IntentTitle: intentTitle,
			Description: fmt.Sprintf("Moved to %s status", intent.StatusArchived),
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
