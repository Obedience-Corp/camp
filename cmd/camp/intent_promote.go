package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/fest"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/ui"
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
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

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

	prevStatus := i.Status

	// Move to done status
	result, err := svc.Move(ctx, i.ID, intent.StatusDone)
	if err != nil {
		return fmt.Errorf("failed to update intent status: %w", err)
	}

	fmt.Printf("✓ Intent promoted: %s\n", result.Path)

	// Auto-commit (unless --no-commit)
	if !noCommit {
		commitResult := git.IntentCommitAll(ctx, git.IntentCommitOptions{
			CampaignRoot: campaignRoot,
			CampaignID:   cfg.ID,
			Action:       git.IntentActionPromote,
			IntentTitle:  i.Title,
			Description:  fmt.Sprintf("Promoted from %s to done", prevStatus),
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	// Auto-create festival from promoted intent (best-effort).
	createFestivalFromIntent(ctx, campaignRoot, i)

	return nil
}

// createFestivalFromIntent attempts to create a festival via fest CLI
// and copies the intent file into the festival's ingest directory.
// This is best-effort: failures do not block the promote operation.
func createFestivalFromIntent(ctx context.Context, campaignRoot string, i *intent.Intent) {
	festPath, err := fest.FindFestCLI()
	if err != nil {
		fmt.Println()
		fmt.Println(ui.Dim("Note: fest CLI not found. Skipping automatic festival creation."))
		fmt.Println(ui.Dim("Install fest to enable promote-to-festival automation."))
		fmt.Println()
		fmt.Println("Next step: Create the Festival with:")
		fmt.Printf("  fest create festival --name %q\n", i.Title)
		return
	}

	festivalName := intent.GenerateSlug(i.Title)
	festivalGoal := extractFirstParagraph(i.Content)

	args := []string{"create", "festival", "--type", "standard", "--name", festivalName}
	if festivalGoal != "" {
		args = append(args, "--goal", festivalGoal)
	}

	cmd := exec.CommandContext(ctx, festPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println()
		fmt.Printf("%s festival creation failed: %v\n", ui.WarningIcon(), err)
		fmt.Println("Intent was promoted successfully. Create the festival manually with:")
		fmt.Printf("  fest create festival --type standard --name %q\n", festivalName)
		return
	}

	fmt.Printf("\n%s Festival '%s' created from promoted intent.\n", ui.SuccessIcon(), festivalName)

	// Copy intent file to the festival's ingest directory (best-effort).
	copyIntentToIngest(campaignRoot, festivalName, i)
}

// extractFirstParagraph returns the first non-header paragraph from markdown content.
func extractFirstParagraph(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	paragraphs := strings.Split(content, "\n\n")
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Skip paragraphs that are just markdown headers
		if strings.HasPrefix(p, "#") {
			lines := strings.SplitN(p, "\n", 2)
			if len(lines) > 1 {
				// Header + body in same paragraph: return the body
				return strings.TrimSpace(lines[1])
			}
			// Only a header line, skip to next paragraph
			continue
		}

		return p
	}

	return ""
}

// copyIntentToIngest copies the intent markdown file into the festival's ingest directory.
// This is best-effort: if the copy fails, a warning is printed but the promote succeeds.
func copyIntentToIngest(campaignRoot, festivalName string, i *intent.Intent) {
	if i.Path == "" {
		return
	}

	// fest creates festivals in festivals/active/ by default.
	ingestDir := filepath.Join(campaignRoot, "festivals", "active", festivalName, "001_INGEST", "input_specs")

	if _, err := os.Stat(ingestDir); os.IsNotExist(err) {
		fmt.Printf("%s 001_INGEST/input_specs/ not found in new festival. Creating it.\n", ui.WarningIcon())
		if err := os.MkdirAll(ingestDir, 0755); err != nil {
			fmt.Printf("%s failed to create ingest directory: %v\n", ui.WarningIcon(), err)
			return
		}
	}

	destPath := filepath.Join(ingestDir, filepath.Base(i.Path))
	if err := copyFile(i.Path, destPath); err != nil {
		fmt.Printf("%s failed to copy intent to ingest: %v\n", ui.WarningIcon(), err)
		return
	}

	rel, _ := filepath.Rel(campaignRoot, destPath)
	fmt.Printf("  Intent copied to %s\n", rel)
}

// copyFile copies src to dst, creating the destination file.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
