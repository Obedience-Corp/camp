package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/promote"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var intentPromoteCmd = &cobra.Command{
	Use:   "promote <id>",
	Short: "Promote an intent through the pipeline",
	Long: `Promote an intent to the next pipeline stage.

TARGETS:
  ready      Move from inbox to ready (reviewed/enriched)
  festival   Move from ready to active + create festival (default)
  design     Move from ready to active + create design doc

The intent moves to active status when promoted to festival or design,
because work is just beginning. Use --force to bypass status checks.

Examples:
  camp intent promote add-dark                       Promote ready → festival
  camp intent promote add-dark --target design       Promote ready → design doc
  camp intent promote add-dark --target ready         Promote inbox → ready
  camp intent promote add-dark --force                Force promote from any status`,
	Args: cobra.ExactArgs(1),
	RunE: runIntentPromote,
}

func init() {
	intentCmd.AddCommand(intentPromoteCmd)

	flags := intentPromoteCmd.Flags()
	flags.String("target", "festival", "Promote target: ready, festival, design")
	flags.Bool("force", false, "Promote even if not in expected status")
	flags.Bool("dry-run", false, "Preview promotion without making changes")
	flags.Bool("no-commit", false, "Don't create a git commit")
}

func runIntentPromote(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]

	// Parse flags
	targetStr, _ := cmd.Flags().GetString("target")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	// Validate target
	var target promote.Target
	switch targetStr {
	case "ready":
		target = promote.TargetReady
	case "festival":
		target = promote.TargetFestival
	case "design":
		target = promote.TargetDesign
	default:
		return fmt.Errorf("invalid target: %s (use ready, festival, or design)", targetStr)
	}

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Find the intent
	i, err := svc.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("intent not found: %s", id)
	}

	// Dry run mode
	if dryRun {
		fmt.Println("Dry run - no changes made")
		fmt.Printf("Would promote intent: %s\n", i.ID)
		fmt.Printf("Title: %s\n", i.Title)
		fmt.Printf("Target: %s\n", target)
		fmt.Printf("Current status: %s\n", i.Status)
		return nil
	}

	prevStatus := i.Status

	result, err := promote.Promote(ctx, svc, i, promote.Options{
		CampaignRoot: campaignRoot,
		Target:       target,
		Force:        force,
	})
	if err != nil {
		return camperrors.Wrap(err, "promotion failed")
	}

	fmt.Printf("%s Intent promoted to %s\n", ui.SuccessIcon(), result.NewStatus)

	promotedTo := result.DesignDir
	if promotedTo == "" {
		promotedTo = result.FestivalDir
	}

	// Log audit event
	if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
		Type:       audit.EventPromote,
		ID:         i.ID,
		Title:      i.Title,
		From:       string(prevStatus),
		To:         string(result.NewStatus),
		PromotedTo: promotedTo,
	}); err != nil {
		return err
	}

	// Auto-commit (unless --no-commit)
	if !noCommit {
		files := []string{i.Path}

		movedIntent, findErr := svc.Get(ctx, i.ID)
		if findErr == nil && movedIntent.Path != "" {
			files = append(files, movedIntent.Path)
		}
		if result.FestivalCreated && result.FestivalDest != "" && result.FestivalDir != "" {
			files = append(files, filepath.Join("festivals", result.FestivalDest, result.FestivalDir))
		}
		if result.DesignCreated && result.DesignDir != "" {
			files = append(files, result.DesignDir)
		}

		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot:  campaignRoot,
				CampaignID:    cfg.ID,
				Files:         commit.NormalizeFiles(campaignRoot, files...),
				SelectiveOnly: true,
			},
			Action:      commit.IntentPromote,
			IntentTitle: i.Title,
			Description: fmt.Sprintf("Promoted from %s to %s", prevStatus, result.NewStatus),
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	// Report outcome based on target.
	switch target {
	case promote.TargetReady:
		// Simple advancement, already reported above.

	case promote.TargetFestival:
		if result.FestNotFound {
			fmt.Println()
			fmt.Println(ui.Dim("Note: fest CLI not found. Skipping automatic festival creation."))
			fmt.Println(ui.Dim("Install fest to enable promote-to-festival automation."))
			fmt.Println()
			fmt.Println("Next step: Create the Festival with:")
			fmt.Printf("  fest create festival --name %q\n", i.Title)
		} else if result.FestivalCreated {
			fmt.Printf("\n%s Festival '%s' created from promoted intent.\n", ui.SuccessIcon(), result.FestivalDir)
			if result.IntentCopied {
				fmt.Printf("  Intent copied to %s/%s/001_INGEST/input_specs/\n", result.FestivalDest, result.FestivalDir)
			}
		} else {
			fmt.Println()
			fmt.Printf("%s festival creation failed\n", ui.WarningIcon())
			fmt.Println("Intent was promoted successfully. Create the festival manually with:")
			fmt.Printf("  fest create festival --type standard --name %q\n", result.FestivalName)
		}

	case promote.TargetDesign:
		if result.DesignCreated {
			fmt.Printf("\n%s Design doc created at %s/\n", ui.SuccessIcon(), result.DesignDir)
		} else {
			fmt.Println()
			fmt.Printf("%s design doc creation failed\n", ui.WarningIcon())
			fmt.Println("Intent was promoted to active. Create the design doc manually.")
		}
	}

	return nil
}
