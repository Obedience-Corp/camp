package main

import (
	"fmt"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/promote"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var intentPromoteCmd = &cobra.Command{
	Use:   "promote <id>",
	Short: "Promote an intent to a Festival",
	Long: `Promote a ready intent to a Festival.

The intent should be in 'ready' status before promotion. Use --force to
promote from any status.

After promotion, the intent will be moved to 'done' status with a reference
to the created Festival.

Examples:
  camp intent promote add-dark           Promote by partial ID
  camp intent promote add-dark --force   Force promote from any status
  camp intent promote add-dark --dry-run Preview without changes`,
	Args: cobra.ExactArgs(1),
	RunE: runIntentPromote,
}

func init() {
	intentCmd.AddCommand(intentPromoteCmd)

	flags := intentPromoteCmd.Flags()
	flags.Bool("force", false, "Promote even if not in ready status")
	flags.Bool("dry-run", false, "Preview promotion without making changes")
	flags.Bool("no-commit", false, "Don't create a git commit")
}

func runIntentPromote(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]

	// Parse flags
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

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

	// Check status (early check for better error message with --force hint)
	if i.Status != intent.StatusReady && !force {
		return fmt.Errorf("intent is not ready for promotion (status: %s)\nUse --force to promote anyway", i.Status)
	}

	// Dry run mode
	if dryRun {
		fmt.Println("Dry run - no changes made")
		fmt.Printf("Would promote intent: %s\n", i.ID)
		fmt.Printf("Title: %s\n", i.Title)
		fmt.Printf("Type: %s\n", i.Type)
		fmt.Println("\nNext steps after promotion:")
		fmt.Println("  1. Festival will be created via fest CLI")
		fmt.Println("  2. Intent will be moved to 'done' status")
		return nil
	}

	prevStatus := i.Status

	result, err := promote.Promote(ctx, svc, i, promote.Options{
		CampaignRoot: campaignRoot,
		Force:        force,
	})
	if err != nil {
		return camperrors.Wrap(err, "promotion failed")
	}

	fmt.Printf("%s Intent promoted to done\n", ui.SuccessIcon())

	// Auto-commit (unless --no-commit)
	if !noCommit {
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot: campaignRoot,
				CampaignID:   cfg.ID,
			},
			Action:      commit.IntentPromote,
			IntentTitle: i.Title,
			Description: fmt.Sprintf("Promoted from %s to done", prevStatus),
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	// Report festival creation outcome.
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

	return nil
}
