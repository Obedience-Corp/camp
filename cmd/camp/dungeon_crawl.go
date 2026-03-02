package main

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/ui"
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
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return camperrors.Wrap(err, "getting current directory")
	}
	dungeonPath := filepath.Join(cwd, "dungeon")

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

	var triageSummary *dungeon.CrawlSummary
	var innerSummary *dungeon.CrawlSummary

	// Run triage crawl if needed
	if runTriage {
		parentItems, err := svc.ListParentItems(ctx, cwd)
		if err != nil {
			return camperrors.Wrap(err, "listing parent items")
		}
		if len(parentItems) > 0 {
			fmt.Printf("%s Triage crawl: %d parent item(s) to review...\n\n", ui.InfoIcon(), len(parentItems))
			triageSummary, err = dungeon.RunTriageCrawl(ctx, svc, cwd)
			if err != nil {
				return camperrors.Wrap(err, "triage crawl failed")
			}
		} else {
			fmt.Printf("%s No parent items to triage.\n", ui.InfoIcon())
		}
	}

	// Run inner crawl if needed
	if runInner {
		dungeonItems, err := svc.ListItems(ctx)
		if err != nil {
			return camperrors.Wrap(err, "listing dungeon items")
		}
		if len(dungeonItems) > 0 {
			if runTriage {
				fmt.Println()
			}
			fmt.Printf("%s Inner crawl: %d dungeon item(s) to review...\n\n", ui.InfoIcon(), len(dungeonItems))
			innerSummary, err = dungeon.RunCrawl(ctx, svc)
			if err != nil {
				return camperrors.Wrap(err, "inner crawl failed")
			}
		} else {
			fmt.Printf("%s Dungeon is empty. Nothing to crawl.\n", ui.InfoIcon())
		}
	}

	// Display summary
	displayCrawlSummary(triageSummary, innerSummary)

	// Autocommit if anything was moved
	commitCrawlChanges(ctx, cfg, campaignRoot, cwd, triageSummary, innerSummary)

	return nil
}

// commitCrawlChanges creates a git commit if any items were moved during crawl.
func commitCrawlChanges(ctx context.Context, cfg *config.CampaignConfig, campaignRoot, cwd string, triage, inner *dungeon.CrawlSummary) {
	hasMoves := (triage != nil && triage.HasMoves()) || (inner != nil && inner.HasMoves())
	if !hasMoves {
		return
	}

	description := buildCrawlCommitMessage(campaignRoot, cwd, triage, inner)

	result := commit.Crawl(ctx, commit.CrawlOptions{
		Options: commit.Options{
			CampaignRoot: campaignRoot,
			CampaignID:   cfg.ID,
		},
		Description: description,
	})

	if result.Committed {
		fmt.Printf("\n%s %s\n", ui.SuccessIcon(), result.Message)
	} else if result.Message != "" {
		fmt.Printf("\n%s %s\n", ui.InfoIcon(), result.Message)
	}
}

// buildCrawlCommitMessage builds the commit body listing moved items
// with paths relative to the campaign root.
func buildCrawlCommitMessage(campaignRoot, cwd string, triage, inner *dungeon.CrawlSummary) string {
	relDir, err := filepath.Rel(campaignRoot, cwd)
	if err != nil {
		relDir = cwd
	}

	var b strings.Builder

	writeMoves := func(summary *dungeon.CrawlSummary, prefix string) {
		if summary == nil || !summary.HasMoves() {
			return
		}

		statuses := make([]string, 0, len(summary.MovedItems))
		for status := range summary.MovedItems {
			statuses = append(statuses, status)
		}
		sort.Strings(statuses)

		for _, status := range statuses {
			items := summary.MovedItems[status]
			fmt.Fprintf(&b, "Moved to %s/%s:\n", prefix, status)
			for _, name := range items {
				fmt.Fprintf(&b, "  - %s/%s\n", relDir, name)
			}
			b.WriteString("\n")
		}
	}

	writeMoves(triage, "dungeon")
	writeMoves(inner, "dungeon")

	return strings.TrimRight(b.String(), "\n")
}

func displayCrawlSummary(triage *dungeon.CrawlSummary, inner *dungeon.CrawlSummary) {
	if triage == nil && inner == nil {
		return
	}

	fmt.Println()
	fmt.Printf("%s Crawl complete!\n", ui.SuccessIcon())

	if triage != nil && triage.Total() > 0 {
		fmt.Printf("\n  Triage (Parent Items):\n")
		printSummaryCounts(triage)
	}

	if inner != nil && inner.Total() > 0 {
		fmt.Printf("\n  Inner Crawl (Dungeon Items):\n")
		printSummaryCounts(inner)
	}
}

func printSummaryCounts(s *dungeon.CrawlSummary) {
	// Print status moves sorted alphabetically
	if len(s.StatusCounts) > 0 {
		statuses := make([]string, 0, len(s.StatusCounts))
		for status := range s.StatusCounts {
			statuses = append(statuses, status)
		}
		sort.Strings(statuses)
		for _, status := range statuses {
			fmt.Printf("  %s Moved to %s: %d\n", ui.BulletIcon(), status, s.StatusCounts[status])
		}
	}
	if s.Kept > 0 {
		fmt.Printf("  %s Kept:    %d\n", ui.BulletIcon(), s.Kept)
	}
	if s.Skipped > 0 {
		fmt.Printf("  %s Skipped: %d\n", ui.BulletIcon(), s.Skipped)
	}
}
