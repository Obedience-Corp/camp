package dungeon

import (
	"context"
	"errors"
	"fmt"
	"sort"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
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
Triage mode includes a route-to-docs action for existing campaign-root docs/<subdirectory>.
Statistics are gathered when available (requires scc or fest).
All decisions are logged to crawl.jsonl for history.

Examples:
  camp dungeon crawl            Auto-detect mode
  camp dungeon crawl --triage   Force triage mode only
  camp dungeon crawl --inner    Force inner mode only`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "requires interactive terminal",
		"interactive":   "true",
	},
	RunE: runDungeonCrawl,
}

func init() {
	Cmd.AddCommand(dungeonCrawlCmd)
	dungeonCrawlCmd.Flags().Bool("triage", false, "Force triage mode (review parent items)")
	dungeonCrawlCmd.Flags().Bool("inner", false, "Force inner mode (review dungeon items)")
}

func runDungeonCrawl(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	triageFlag, _ := cmd.Flags().GetBool("triage")
	innerFlag, _ := cmd.Flags().GetBool("inner")

	cmdCtx, err := resolveDungeonCommandContext(ctx)
	if err != nil {
		return err
	}
	if !ui.IsTerminal() {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "dungeon crawl requires an interactive terminal")
	}

	svc := intdungeon.NewService(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)
	// Defer external markdown-link rewriting so the campaign tree is scanned
	// once after all moves, not once per move (which is O(moves x workspace)
	// and dominates wall-clock time on large workspaces).
	svc.BeginLinkBatch()
	relParent := RelFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath)
	relDungeon := RelFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)

	// Determine modes
	runTriage, runInner := triageFlag, innerFlag
	if !triageFlag && !innerFlag {
		// Auto-detect
		parentItems, _ := svc.ListParentItems(ctx, cmdCtx.Dungeon.ParentPath)
		dungeonItems, _ := svc.ListItems(ctx)
		runTriage = len(parentItems) > 0
		runInner = len(dungeonItems) > 0
	}

	if !runTriage && !runInner {
		fmt.Printf("%s Nothing to crawl in context parent=%s dungeon=%s.\n", ui.InfoIcon(), relParent, relDungeon)
		return nil
	}

	var triageSummary *intdungeon.CrawlSummary
	var innerSummary *intdungeon.CrawlSummary
	aborted := false

	// Run triage crawl if needed
	if runTriage {
		parentItems, err := svc.ListParentItems(ctx, cmdCtx.Dungeon.ParentPath)
		if err != nil {
			return camperrors.Wrap(err, "listing parent items")
		}
		if len(parentItems) > 0 {
			fmt.Printf(
				"%s Triage crawl: %d parent item(s) to review (parent=%s -> dungeon=%s)...\n\n",
				ui.InfoIcon(),
				len(parentItems),
				relParent,
				relDungeon,
			)
			triageSummary, err = intdungeon.RunTriageCrawl(ctx, svc, cmdCtx.Dungeon.ParentPath)
			if err != nil {
				if errors.Is(err, intdungeon.ErrCrawlAborted) {
					aborted = true
				} else {
					return camperrors.Wrap(err, "triage crawl failed")
				}
			}
		} else {
			fmt.Printf("%s No parent items to triage in %s.\n", ui.InfoIcon(), relParent)
		}
	}

	// flushLinkRewrites applies the deferred external-link rewrites for every
	// item moved so far in a single campaign-tree scan.
	flushLinkRewrites := func() error {
		if (triageSummary != nil && triageSummary.HasMoves()) ||
			(innerSummary != nil && innerSummary.HasMoves()) {
			fmt.Printf("%s Updating markdown cross-references...\n", ui.InfoIcon())
		}
		return svc.FlushLinkRewrites(ctx)
	}

	// Run inner crawl if the triage step did not abort.
	if !aborted && runInner {
		dungeonItems, err := svc.ListItems(ctx)
		if err != nil {
			return camperrors.Wrap(err, "listing dungeon items")
		}
		if len(dungeonItems) > 0 {
			if runTriage {
				fmt.Println()
			}
			fmt.Printf(
				"%s Inner crawl: %d dungeon item(s) to review in %s...\n\n",
				ui.InfoIcon(),
				len(dungeonItems),
				relDungeon,
			)
			innerSummary, err = intdungeon.RunCrawl(ctx, svc)
			if err != nil {
				if errors.Is(err, intdungeon.ErrCrawlAborted) {
					aborted = true
				} else {
					return camperrors.Wrap(err, "inner crawl failed")
				}
			}
		} else {
			fmt.Printf("%s Dungeon is empty in %s. Nothing to crawl.\n", ui.InfoIcon(), relDungeon)
		}
	}

	if aborted {
		// Apply rewrites for items moved before the abort so the on-disk state
		// stays consistent, but do not auto-commit a cancelled crawl.
		_ = flushLinkRewrites()
		displayCrawlSummary(fmt.Sprintf("%s Crawl cancelled.\n", ui.InfoIcon()), triageSummary, innerSummary)
		return nil
	}

	if err := flushLinkRewrites(); err != nil {
		return camperrors.Wrap(err, "updating markdown cross-references")
	}

	// Display summary
	displayCrawlSummary(fmt.Sprintf("%s Crawl complete!\n", ui.SuccessIcon()), triageSummary, innerSummary)

	// Autocommit if anything was moved
	if err := commitCrawlChanges(ctx, cmdCtx, svc, triageSummary, innerSummary); err != nil {
		return err
	}

	return nil
}

// commitCrawlChanges creates a git commit if any items were moved during crawl.
func commitCrawlChanges(ctx context.Context, cmdCtx *dungeonCommandContext, svc *intdungeon.Service, triage, inner *intdungeon.CrawlSummary) error {
	plan, err := intdungeon.PrepareCrawlCommit(ctx, cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath, cmdCtx.Dungeon.DungeonPath, svc.RewrittenLinkFiles(), triage, inner)
	if err != nil {
		return err
	}
	if plan == nil {
		return nil
	}

	crawlID, err := commit.NewCrawlID()
	if err != nil {
		crawlID = ""
	}

	result := commit.Crawl(ctx, commit.CrawlOptions{
		Options: commit.Options{
			CampaignRoot: cmdCtx.CampaignRoot,
			CampaignID:   cmdCtx.Config.ID,
			PreStaged:    plan.PreStaged,
		},
		CrawlID:     crawlID,
		Description: plan.Description,
		Files:       plan.Files,
	})

	if result.Committed {
		fmt.Printf("\n%s %s\n", ui.SuccessIcon(), result.Message)
		return nil
	}
	if result.NoChanges {
		fmt.Printf("\n%s %s\n", ui.InfoIcon(), result.Message)
		return nil
	}
	if result.Err != nil {
		fmt.Printf("\n%s Crawl changes were applied on disk, but auto-commit failed.\n", ui.WarningIcon())
		fmt.Printf("%s %v\n", ui.WarningIcon(), result.Err)
		return camperrors.Wrap(result.Err, "auto-committing crawl changes")
	}
	if result.Message != "" {
		fmt.Printf("\n%s %s\n", ui.InfoIcon(), result.Message)
	}
	return nil
}

func displayCrawlSummary(header string, triage *intdungeon.CrawlSummary, inner *intdungeon.CrawlSummary) {
	if triage == nil && inner == nil {
		return
	}

	fmt.Println()
	fmt.Print(header)

	if triage != nil && triage.Total() > 0 {
		fmt.Printf("\n  Triage (Parent Items):\n")
		printSummaryCounts(triage)
	}

	if inner != nil && inner.Total() > 0 {
		fmt.Printf("\n  Inner Crawl (Dungeon Items):\n")
		printSummaryCounts(inner)
	}
}

func printSummaryCounts(s *intdungeon.CrawlSummary) {
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
