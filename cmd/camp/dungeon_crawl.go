package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/dungeon"
	"github.com/obediencecorp/camp/internal/ui"
)

var dungeonCrawlCmd = &cobra.Command{
	Use:   "crawl",
	Short: "Interactive dungeon review",
	Long: `Interactively review and archive dungeon contents.

For each item in the dungeon root, you'll be prompted to:
  - Keep: Leave it in the dungeon for later review
  - Archive: Move to archived/ (truly out of the way)
  - Skip: Come back to it another time
  - Quit: Stop the crawl session

Statistics are gathered when available (requires scc or fest).
All decisions are logged to crawl.jsonl for history.

Items in archived/ are excluded from the crawl.

Examples:
  camp dungeon crawl    Start interactive review`,
	Args: cobra.NoArgs,
	RunE: runDungeonCrawl,
}

func init() {
	dungeonCmd.AddCommand(dungeonCrawlCmd)
}

func runDungeonCrawl(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load campaign config
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Get current working directory for local dungeon
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}
	dungeonPath := filepath.Join(cwd, "dungeon")

	// Create dungeon service (still validates campaign, but uses CWD for dungeon)
	_ = cfg // campaign config loaded to validate we're in a campaign
	svc := dungeon.NewService(campaignRoot, dungeonPath)

	// Check if dungeon exists
	items, err := svc.ListItems(ctx)
	if err != nil {
		return fmt.Errorf("listing dungeon items: %w", err)
	}

	if len(items) == 0 {
		fmt.Printf("%s Dungeon is empty. Nothing to crawl.\n", ui.InfoIcon())
		fmt.Printf("\n  Move items to %s to start using the dungeon.\n", ui.Value("./dungeon"))
		return nil
	}

	fmt.Printf("%s Starting dungeon crawl with %d item(s)...\n\n", ui.InfoIcon(), len(items))

	// Run the crawl
	summary, err := dungeon.RunCrawl(ctx, svc)
	if err != nil {
		return fmt.Errorf("crawl failed: %w", err)
	}

	// Display summary
	fmt.Println()
	fmt.Printf("%s Crawl complete!\n", ui.SuccessIcon())
	fmt.Printf("  %s Kept: %d\n", ui.BulletIcon(), summary.Kept)
	fmt.Printf("  %s Archived: %d\n", ui.BulletIcon(), summary.Archived)
	fmt.Printf("  %s Skipped: %d\n", ui.BulletIcon(), summary.Skipped)

	if summary.Archived > 0 {
		fmt.Printf("\n%s Archived items moved to %s\n", ui.InfoIcon(), ui.Value("./dungeon/archived/"))
	}

	return nil
}
