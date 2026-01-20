package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
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
}

func runIntentPromote(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]

	// Parse flags
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Find campaign root
	_, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create service
	svc := intent.NewIntentService(campaignRoot)

	// Find the intent
	i, err := svc.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("intent not found: %s", id)
	}

	// Check status
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
		fmt.Println("  1. Run 'fest create festival' to create the Festival")
		fmt.Println("  2. Intent will be moved to 'done' status")
		return nil
	}

	// Move to done status
	result, err := svc.Move(ctx, i.ID, intent.StatusDone)
	if err != nil {
		return fmt.Errorf("failed to update intent status: %w", err)
	}

	fmt.Printf("✓ Intent promoted: %s\n", result.Path)
	fmt.Println("\nNext step: Create the Festival with:")
	fmt.Printf("  fest create festival --name \"%s\"\n", i.Title)

	return nil
}
