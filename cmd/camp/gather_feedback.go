package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent/feedback"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var (
	gatherFeedbackFestivalID string
	gatherFeedbackStatus     string
	gatherFeedbackSeverity   string
	gatherFeedbackForce      bool
	gatherFeedbackDryRun     bool
	gatherFeedbackNoCommit   bool
)

var gatherFeedbackCmd = &cobra.Command{
	Use:   "feedback",
	Short: "Gather feedback observations from festivals into intents",
	Long: `Scan festival directories for feedback observations and create
trackable FEEDBACK intent files with checkboxes.

Each festival with feedback observations gets a FEEDBACK_<fest_id>.md intent
in workflow/intents/inbox/. Observations are grouped by criteria with
checkboxes for tracking addressed status.

Deduplication tracking ensures observations are only gathered once.
Re-running the command appends only new observations to existing intents,
preserving any checkbox state from previous runs.

Examples:
  # Gather all feedback from all festivals
  camp gather feedback

  # Preview what would be gathered
  camp gather feedback --dry-run

  # Gather from a specific festival
  camp gather feedback --festival-id CC0004

  # Only gather from completed festivals
  camp gather feedback --status completed

  # Filter by severity
  camp gather feedback --severity high

  # Re-gather everything (ignore tracking)
  camp gather feedback --force`,
	RunE: runGatherFeedback,
}

func init() {
	gatherCmd.AddCommand(gatherFeedbackCmd)

	gatherFeedbackCmd.Flags().StringVar(&gatherFeedbackFestivalID, "festival-id", "", "Only gather from a specific festival")
	gatherFeedbackCmd.Flags().StringVar(&gatherFeedbackStatus, "status", "completed,active,planned", "Festival status dirs to scan (comma-separated)")
	gatherFeedbackCmd.Flags().StringVar(&gatherFeedbackSeverity, "severity", "", "Filter by observation severity (low, medium, high)")
	gatherFeedbackCmd.Flags().BoolVar(&gatherFeedbackForce, "force", false, "Re-gather all, ignoring tracking")
	gatherFeedbackCmd.Flags().BoolVar(&gatherFeedbackDryRun, "dry-run", false, "Preview without creating intents")
	gatherFeedbackCmd.Flags().BoolVar(&gatherFeedbackNoCommit, "no-commit", false, "Skip git commit")
}

func runGatherFeedback(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load campaign config
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)

	// Parse status flags
	statuses := strings.Split(gatherFeedbackStatus, ",")
	for i := range statuses {
		statuses[i] = strings.TrimSpace(statuses[i])
	}

	opts := feedback.GatherOptions{
		FestivalID: gatherFeedbackFestivalID,
		Statuses:   statuses,
		Severity:   gatherFeedbackSeverity,
		Force:      gatherFeedbackForce,
		DryRun:     gatherFeedbackDryRun,
		NoCommit:   gatherFeedbackNoCommit,
	}

	// Scan festivals for feedback
	scanner := feedback.NewScanner(resolver.Festivals())
	festivals, err := scanner.Scan(ctx, opts)
	if err != nil {
		return fmt.Errorf("scanning festivals: %w", err)
	}

	if len(festivals) == 0 {
		fmt.Println("No festivals with feedback observations found.")
		return nil
	}

	// Filter to new observations using tracker
	tracker := feedback.NewTracker()
	result, err := gatherNewFeedback(ctx, festivals, tracker, feedback.NewBuilder(resolver.Intents()), opts)
	if err != nil {
		return err
	}

	// Print results
	printGatherResult(result, opts.DryRun)

	// Git commit
	if !opts.DryRun && !opts.NoCommit && (result.IntentsCreated > 0 || result.IntentsUpdated > 0) {
		commitGatherFeedback(ctx, campaignRoot, cfg.ID, result)
	}

	return nil
}

func gatherNewFeedback(ctx context.Context, festivals []feedback.FestivalFeedback, tracker *feedback.Tracker, builder *feedback.Builder, opts feedback.GatherOptions) (*feedback.GatherResult, error) {
	result := &feedback.GatherResult{}

	for _, fest := range festivals {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled: %w", err)
		}

		result.FestivalsScanned++

		// Filter new observations
		newObs, err := tracker.FilterNew(fest.Festival.Path, fest.Observations, opts.Force)
		if err != nil {
			return nil, fmt.Errorf("checking tracking for %s: %w", fest.Festival.ID, err)
		}

		festResult := feedback.FestivalGatherResult{
			Festival: fest.Festival,
			TotalObs: len(fest.Observations),
			NewObs:   len(newObs),
		}

		if len(newObs) == 0 {
			result.FestivalResults = append(result.FestivalResults, festResult)
			continue
		}

		result.NewObservations += len(newObs)

		if opts.DryRun {
			result.FestivalResults = append(result.FestivalResults, festResult)
			continue
		}

		// Build or update intent file
		intentPath, created, err := builder.BuildOrUpdate(fest.Festival, newObs)
		if err != nil {
			return nil, fmt.Errorf("building intent for %s: %w", fest.Festival.ID, err)
		}

		festResult.IntentFile = intentPath
		festResult.IntentCreated = created
		festResult.IntentUpdated = !created

		if created {
			result.IntentsCreated++
		} else {
			result.IntentsUpdated++
		}

		// Record gathered observations in tracking file
		if err := tracker.RecordGathered(fest.Festival.Path, newObs); err != nil {
			return nil, fmt.Errorf("recording tracking for %s: %w", fest.Festival.ID, err)
		}

		result.FestivalResults = append(result.FestivalResults, festResult)
	}

	return result, nil
}

func printGatherResult(result *feedback.GatherResult, dryRun bool) {
	if dryRun {
		fmt.Println("Dry run — no changes made.")
		fmt.Println()
	}

	fmt.Printf("Scanned %d festivals with feedback\n", result.FestivalsScanned)

	if result.NewObservations == 0 {
		fmt.Println("No new feedback to gather.")
		return
	}

	fmt.Printf("Found %d new observations\n\n", result.NewObservations)

	for _, fr := range result.FestivalResults {
		if fr.NewObs == 0 {
			continue
		}

		action := "would create"
		if !dryRun {
			if fr.IntentCreated {
				action = "created"
			} else {
				action = "updated"
			}
		}

		fmt.Printf("  %s (%s): %d new observations — %s FEEDBACK_%s.md\n",
			fr.Festival.Name, fr.Festival.ID, fr.NewObs, action, fr.Festival.ID)
	}

	if !dryRun {
		fmt.Printf("\nCreated: %d, Updated: %d\n", result.IntentsCreated, result.IntentsUpdated)
	}
}

func commitGatherFeedback(ctx context.Context, campaignRoot, campaignID string, result *feedback.GatherResult) {
	var ids []string
	for _, fr := range result.FestivalResults {
		if fr.NewObs > 0 {
			ids = append(ids, fr.Festival.ID)
		}
	}

	description := fmt.Sprintf("Gathered %d observations from %d festivals: %s",
		result.NewObservations, len(ids), strings.Join(ids, ", "))

	commitResult := commit.Intent(ctx, commit.IntentOptions{
		Options: commit.Options{
			CampaignRoot: campaignRoot,
			CampaignID:   campaignID,
		},
		Action:      commit.IntentGather,
		IntentTitle: "Gather festival feedback",
		Description: description,
	})
	if commitResult.Message != "" {
		fmt.Printf("  %s\n", commitResult.Message)
	}
}
