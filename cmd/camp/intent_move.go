package main

import (
	"fmt"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentMoveCmd = &cobra.Command{
	Use:   "move <id> <status>",
	Short: "Move intent to a different status",
	Long: `Transition an intent between lifecycle statuses.

VALID STATUSES:
  inbox      Captured, not yet reviewed
  ready      Reviewed/enriched, ready to be promoted
  active     Promoted to festival/design, work in progress
  done       Resolved (dungeon)
  killed     Abandoned (dungeon)
  archived   Preserved but inactive (dungeon)
  someday    Deferred (dungeon)

PIPELINE ORDER:
  inbox → ready → active → dungeon/done

Move is an escape hatch that allows any-to-any transitions.
Dungeon moves require a --reason flag.

Examples:
  camp intent move add-dark ready                         Mark as ready
  camp intent move add-dark done --reason "completed"     Mark as done
  camp intent move add-dark killed --reason "superseded"  Kill intent`,
	Args: cobra.ExactArgs(2),
	RunE: runIntentMove,
}

func init() {
	intentCmd.AddCommand(intentMoveCmd)

	intentMoveCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
	intentMoveCmd.Flags().String("reason", "", "Reason for the move (required for dungeon targets)")
}

func runIntentMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	newStatus := args[1]
	noCommit, _ := cmd.Flags().GetBool("no-commit")
	reason, _ := cmd.Flags().GetString("reason")

	// Validate status — accept short names for dungeon statuses
	var status intent.Status
	switch newStatus {
	case "inbox":
		status = intent.StatusInbox
	case "ready":
		status = intent.StatusReady
	case "active":
		status = intent.StatusActive
	case "done":
		status = intent.StatusDone
	case "killed":
		status = intent.StatusKilled
	case "archived":
		status = intent.StatusArchived
	case "someday":
		status = intent.StatusSomeday
	default:
		return fmt.Errorf("invalid status: %s (use inbox, ready, active, done, killed, archived, or someday)", newStatus)
	}

	// Require reason for dungeon moves
	if status.InDungeon() && reason == "" {
		return fmt.Errorf("--reason is required when moving to a dungeon status (%s)", status)
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

	// Append decision record for dungeon moves
	if status.InDungeon() && reason != "" {
		intent.AppendDecisionRecord(i, status, reason)
		_ = svc.Save(ctx, i)
	}

	// Move the intent
	result, err := svc.Move(ctx, id, status)
	if err != nil {
		return camperrors.Wrap(err, "failed to move intent")
	}

	fmt.Printf("✓ Intent moved to %s: %s\n", status, result.Path)

	// Log audit event
	audit.AppendEvent(ctx, resolver.Intents(), audit.Event{
		Type:  audit.EventMove,
		ID:    i.ID,
		Title: intentTitle,
		From:  string(i.Status),
		To:    string(status),
		Reason: reason,
	})

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
