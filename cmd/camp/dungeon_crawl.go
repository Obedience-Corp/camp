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

Without flags, auto-detects what to crawl:
  - Parent items exist → triage mode (move items into dungeon)
  - Dungeon items exist → inner mode (keep/archive dungeon items)
  - Both exist → runs triage first, then inner

Use --triage or --inner to force a specific mode.

For each item, you'll be prompted to decide its fate.
Statistics are gathered when available (requires scc or fest).
All decisions are logged to crawl.jsonl for history.

Examples:
  camp dungeon crawl            Auto-detect mode
  camp dungeon crawl --triage   Force triage mode only
  camp dungeon crawl --inner    Force inner mode only`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Interactive review session",
		"interactive":   "true",
	},
	RunE: runDungeonCrawl,
}

func init() {
	dungeonCmd.AddCommand(dungeonCrawlCmd)
	dungeonCrawlCmd.Flags().Bool("triage", false, "Force triage mode (review parent items)")
	dungeonCrawlCmd.Flags().Bool("inner", false, "Force inner mode (review dungeon items)")
}

func runDungeonCrawl(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	triageFlag, _ := cmd.Flags().GetBool("triage")
	innerFlag, _ := cmd.Flags().GetBool("inner")

	// Load campaign config
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}
	dungeonPath := filepath.Join(cwd, "dungeon")

	_ = cfg
	svc := dungeon.NewService(campaignRoot, dungeonPath)

	// Determine modes
	runTriage, runInner := triageFlag, innerFlag
	if !triageFlag && !innerFlag {
		// Auto-detect
		parentItems, _ := svc.ListParentItems(ctx, cwd)
		dungeonItems, _ := svc.ListItems(ctx)
		runTriage = len(parentItems) > 0
		runInner = len(dungeonItems) > 0
	}

	if !runTriage && !runInner {
		fmt.Printf("%s Nothing to crawl: no parent items or dungeon items found.\n", ui.InfoIcon())
		return nil
	}

	var triageSummary *dungeon.TriageSummary
	var innerSummary *dungeon.CrawlSummary

	// Run triage crawl if needed
	if runTriage {
		parentItems, err := svc.ListParentItems(ctx, cwd)
		if err != nil {
			return fmt.Errorf("listing parent items: %w", err)
		}
		if len(parentItems) > 0 {
			fmt.Printf("%s Triage crawl: %d parent item(s) to review...\n\n", ui.InfoIcon(), len(parentItems))
			triageSummary, err = dungeon.RunTriageCrawl(ctx, svc, cwd)
			if err != nil {
				return fmt.Errorf("triage crawl failed: %w", err)
			}
		} else {
			fmt.Printf("%s No parent items to triage.\n", ui.InfoIcon())
		}
	}

	// Run inner crawl if needed
	if runInner {
		dungeonItems, err := svc.ListItems(ctx)
		if err != nil {
			return fmt.Errorf("listing dungeon items: %w", err)
		}
		if len(dungeonItems) > 0 {
			if runTriage {
				fmt.Println()
			}
			fmt.Printf("%s Inner crawl: %d dungeon item(s) to review...\n\n", ui.InfoIcon(), len(dungeonItems))
			innerSummary, err = dungeon.RunCrawl(ctx, svc)
			if err != nil {
				return fmt.Errorf("inner crawl failed: %w", err)
			}
		} else {
			fmt.Printf("%s Dungeon is empty. Nothing to crawl.\n", ui.InfoIcon())
		}
	}

	// Display summary
	displayCrawlSummary(triageSummary, innerSummary)

	return nil
}

func displayCrawlSummary(triage *dungeon.TriageSummary, inner *dungeon.CrawlSummary) {
	if triage == nil && inner == nil {
		return
	}

	fmt.Println()
	fmt.Printf("%s Crawl complete!\n", ui.SuccessIcon())

	if triage != nil && triage.Total() > 0 {
		fmt.Printf("\n  Triage (Parent Items):\n")
		fmt.Printf("  %s Moved to dungeon: %d\n", ui.BulletIcon(), triage.Moved)
		fmt.Printf("  %s Kept in place:    %d\n", ui.BulletIcon(), triage.Kept)
		fmt.Printf("  %s Skipped:          %d\n", ui.BulletIcon(), triage.Skipped)
	}

	if inner != nil && inner.Total() > 0 {
		fmt.Printf("\n  Inner Crawl (Dungeon Items):\n")
		fmt.Printf("  %s Kept:     %d\n", ui.BulletIcon(), inner.Kept)
		fmt.Printf("  %s Archived: %d\n", ui.BulletIcon(), inner.Archived)
		fmt.Printf("  %s Skipped:  %d\n", ui.BulletIcon(), inner.Skipped)

		if inner.Archived > 0 {
			fmt.Printf("\n%s Archived items moved to %s\n", ui.InfoIcon(), ui.Value("./dungeon/archived/"))
		}
	}
}
