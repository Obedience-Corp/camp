package main

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/dungeon"
	"github.com/Obedience-Corp/camp/internal/git"
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
Triage mode includes a route-to-docs action for campaign-root docs/<subdirectory>.
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

	cmdCtx, err := resolveDungeonCommandContext(ctx)
	if err != nil {
		return err
	}
	svc := dungeon.NewService(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)
	relParent := relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath)
	relDungeon := relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)

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

	var triageSummary *dungeon.CrawlSummary
	var innerSummary *dungeon.CrawlSummary

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
			triageSummary, err = dungeon.RunTriageCrawl(ctx, svc, cmdCtx.Dungeon.ParentPath)
			if err != nil {
				return camperrors.Wrap(err, "triage crawl failed")
			}
		} else {
			fmt.Printf("%s No parent items to triage in %s.\n", ui.InfoIcon(), relParent)
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
			fmt.Printf(
				"%s Inner crawl: %d dungeon item(s) to review in %s...\n\n",
				ui.InfoIcon(),
				len(dungeonItems),
				relDungeon,
			)
			innerSummary, err = dungeon.RunCrawl(ctx, svc)
			if err != nil {
				return camperrors.Wrap(err, "inner crawl failed")
			}
		} else {
			fmt.Printf("%s Dungeon is empty in %s. Nothing to crawl.\n", ui.InfoIcon(), relDungeon)
		}
	}

	// Display summary
	displayCrawlSummary(triageSummary, innerSummary)

	// Autocommit if anything was moved
	commitCrawlChanges(ctx, cmdCtx, triageSummary, innerSummary)

	return nil
}

// commitCrawlChanges creates a git commit if any items were moved during crawl.
func commitCrawlChanges(ctx context.Context, cmdCtx *dungeonCommandContext, triage, inner *dungeon.CrawlSummary) {
	hasMoves := (triage != nil && triage.HasMoves()) || (inner != nil && inner.HasMoves())
	if !hasMoves {
		return
	}

	description := buildCrawlCommitMessage(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath, triage, inner)

	relDungeon, err := filepath.Rel(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)
	if err != nil {
		relDungeon = cmdCtx.Dungeon.DungeonPath
	}

	files := []string{relDungeon}
	files = append(files, crawlDocsDestinationPaths(triage)...)

	// For triage moves, include source deletions in the commit scope.
	// Only include paths git actually tracks to avoid "pathspec did not
	// match" errors when the parent directory was renamed or never committed.
	if triage != nil && triage.HasMoves() {
		relParent, relErr := filepath.Rel(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath)
		if relErr != nil {
			relParent = cmdCtx.Dungeon.ParentPath
		}
		var sourcePaths []string
		for _, names := range triage.MovedItems {
			for _, name := range names {
				sourcePaths = append(sourcePaths, filepath.Join(relParent, name))
			}
		}
		if len(sourcePaths) > 0 {
			tracked, filterErr := git.FilterTracked(ctx, cmdCtx.CampaignRoot, sourcePaths)
			if filterErr != nil {
				fmt.Printf("%s Warning: could not check tracked paths: %v\n", ui.InfoIcon(), filterErr)
			}
			if len(tracked) > 0 {
				if err := git.StageTrackedChanges(ctx, cmdCtx.CampaignRoot, tracked...); err != nil {
					fmt.Printf("%s Warning: could not stage source deletions: %v\n", ui.InfoIcon(), err)
				}
				files = append(files, tracked...)
			}
		}
	}

	result := commit.Crawl(ctx, commit.CrawlOptions{
		Options: commit.Options{
			CampaignRoot: cmdCtx.CampaignRoot,
			CampaignID:   cmdCtx.Config.ID,
		},
		Description: description,
		Files:       files,
	})

	if result.Committed {
		fmt.Printf("\n%s %s\n", ui.SuccessIcon(), result.Message)
	} else if result.Message != "" {
		fmt.Printf("\n%s %s\n", ui.InfoIcon(), result.Message)
	}
}

// buildCrawlCommitMessage builds the commit body listing moved items with paths
// relative to the campaign root.
func buildCrawlCommitMessage(campaignRoot, parentPath string, triage, inner *dungeon.CrawlSummary) string {
	relDir, err := filepath.Rel(campaignRoot, parentPath)
	if err != nil {
		relDir = parentPath
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
			if strings.HasPrefix(status, "docs/") {
				fmt.Fprintf(&b, "Moved to %s:\n", status)
			} else {
				fmt.Fprintf(&b, "Moved to %s/%s:\n", prefix, status)
			}
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

func crawlDocsDestinationPaths(summary *dungeon.CrawlSummary) []string {
	if summary == nil || !summary.HasMoves() {
		return nil
	}

	seen := make(map[string]struct{})
	var paths []string
	for status := range summary.MovedItems {
		if !strings.HasPrefix(status, "docs/") {
			continue
		}
		subpath := strings.TrimPrefix(status, "docs/")
		if subpath == "" || subpath == "." {
			continue
		}
		cleanSubpath := filepath.Clean(subpath)
		if cleanSubpath == "." || cleanSubpath == ".." || strings.HasPrefix(cleanSubpath, ".."+string(filepath.Separator)) {
			continue
		}
		clean := filepath.Join("docs", cleanSubpath)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		paths = append(paths, clean)
	}
	sort.Strings(paths)
	return paths
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
