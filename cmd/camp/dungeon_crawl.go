package main

import (
	"context"
	"errors"
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
			triageSummary, err = dungeon.RunTriageCrawl(ctx, svc, cmdCtx.Dungeon.ParentPath)
			if err != nil {
				if errors.Is(err, dungeon.ErrCrawlAborted) {
					aborted = true
				} else {
					return camperrors.Wrap(err, "triage crawl failed")
				}
			}
		} else {
			fmt.Printf("%s No parent items to triage in %s.\n", ui.InfoIcon(), relParent)
		}
	}

	if aborted {
		displayCrawlSummary(fmt.Sprintf("%s Crawl cancelled.\n", ui.InfoIcon()), triageSummary, innerSummary)
		return nil
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
				if errors.Is(err, dungeon.ErrCrawlAborted) {
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
		displayCrawlSummary(fmt.Sprintf("%s Crawl cancelled.\n", ui.InfoIcon()), triageSummary, innerSummary)
		return nil
	}

	// Display summary
	displayCrawlSummary(fmt.Sprintf("%s Crawl complete!\n", ui.SuccessIcon()), triageSummary, innerSummary)

	// Autocommit if anything was moved
	if err := commitCrawlChanges(ctx, cmdCtx, triageSummary, innerSummary); err != nil {
		return err
	}

	return nil
}

// commitCrawlChanges creates a git commit if any items were moved during crawl.
func commitCrawlChanges(ctx context.Context, cmdCtx *dungeonCommandContext, triage, inner *dungeon.CrawlSummary) error {
	hasMoves := (triage != nil && triage.HasMoves()) || (inner != nil && inner.HasMoves())
	if !hasMoves {
		return nil
	}

	description := buildCrawlCommitMessage(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath, triage, inner)

	relDungeon, err := filepath.Rel(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)
	if err != nil {
		relDungeon = cmdCtx.Dungeon.DungeonPath
	}

	files := crawlCommitPaths(relDungeon, triage, inner)
	preStaged, err := stageTrackedCrawlSourceDeletions(
		ctx,
		cmdCtx.CampaignRoot,
		cmdCtx.Dungeon.ParentPath,
		relDungeon,
		triage,
		inner,
	)
	if err != nil {
		return camperrors.Wrap(err, "staging crawl source deletions")
	}

	result := commit.Crawl(ctx, commit.CrawlOptions{
		Options: commit.Options{
			CampaignRoot: cmdCtx.CampaignRoot,
			CampaignID:   cmdCtx.Config.ID,
			PreStaged:    preStaged,
		},
		Description: description,
		Files:       files,
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

func crawlMovedItemPaths(relDungeon string, summaries ...*dungeon.CrawlSummary) []string {
	seen := make(map[string]struct{})
	var paths []string

	for _, summary := range summaries {
		if summary == nil || !summary.HasMoves() {
			continue
		}
		for status, names := range summary.MovedItems {
			base, ok := crawlMoveDestinationBase(relDungeon, status)
			if !ok {
				continue
			}
			for _, name := range names {
				cleanName, ok := cleanCrawlCommitName(name)
				if !ok {
					continue
				}
				path := filepath.Join(base, cleanName)
				path = filepath.Clean(path)
				if !isSafeCrawlCommitPath(path) {
					continue
				}
				if _, exists := seen[path]; exists {
					continue
				}
				seen[path] = struct{}{}
				paths = append(paths, path)
			}
		}
	}

	sort.Strings(paths)
	return paths
}

func crawlCommitPaths(relDungeon string, summaries ...*dungeon.CrawlSummary) []string {
	paths := crawlMovedItemPaths(relDungeon, summaries...)

	logPath, ok := cleanCrawlCommitPath(filepath.Join(relDungeon, "crawl.jsonl"))
	if !ok || !isSafeCrawlCommitPath(logPath) {
		return paths
	}

	for _, path := range paths {
		if path == logPath {
			return paths
		}
	}

	return append(paths, logPath)
}

func crawlMoveDestinationBase(relDungeon, status string) (string, bool) {
	if status == "docs" || strings.HasPrefix(status, "docs"+string(filepath.Separator)) {
		cleanStatus, ok := cleanCrawlCommitPath(status)
		if !ok {
			return "", false
		}
		if cleanStatus != "docs" && !strings.HasPrefix(cleanStatus, "docs"+string(filepath.Separator)) {
			return "", false
		}
		return cleanStatus, true
	}

	cleanStatus, ok := cleanCrawlCommitPath(status)
	if !ok {
		return "", false
	}

	cleanDungeon, ok := cleanCrawlCommitPath(relDungeon)
	if !ok {
		return "", false
	}
	return filepath.Join(cleanDungeon, cleanStatus), true
}

func stageTrackedCrawlSourceDeletions(
	ctx context.Context,
	campaignRoot string,
	parentPath string,
	relDungeon string,
	triage *dungeon.CrawlSummary,
	inner *dungeon.CrawlSummary,
) ([]string, error) {
	sourcePaths := crawlSourceDeletionPaths(campaignRoot, parentPath, relDungeon, triage, inner)
	if len(sourcePaths) == 0 {
		return nil, nil
	}

	tracked, err := git.FilterTracked(ctx, campaignRoot, sourcePaths)
	if err != nil {
		return nil, err
	}
	if len(tracked) == 0 {
		return nil, nil
	}
	if err := git.StageTrackedChanges(ctx, campaignRoot, tracked...); err != nil {
		return nil, err
	}
	return tracked, nil
}

func crawlSourceDeletionPaths(
	campaignRoot string,
	parentPath string,
	relDungeon string,
	triage *dungeon.CrawlSummary,
	inner *dungeon.CrawlSummary,
) []string {
	seen := make(map[string]struct{})
	var paths []string

	relParent, err := filepath.Rel(campaignRoot, parentPath)
	if err != nil {
		relParent = parentPath
	}

	appendSummary := func(base string, summary *dungeon.CrawlSummary) {
		if summary == nil || !summary.HasMoves() {
			return
		}
		cleanBase := ""
		if base != "." && base != "" {
			var ok bool
			cleanBase, ok = cleanCrawlCommitPath(base)
			if !ok {
				return
			}
		}
		for _, names := range summary.MovedItems {
			for _, name := range names {
				cleanName, ok := cleanCrawlCommitName(name)
				if !ok {
					continue
				}
				path := cleanName
				if cleanBase != "" {
					path = filepath.Join(cleanBase, cleanName)
				}
				path = filepath.Clean(path)
				if !isSafeCrawlCommitPath(path) {
					continue
				}
				if _, exists := seen[path]; exists {
					continue
				}
				seen[path] = struct{}{}
				paths = append(paths, path)
			}
		}
	}

	appendSummary(relParent, triage)
	appendSummary(relDungeon, inner)
	sort.Strings(paths)
	return paths
}

func cleanCrawlCommitName(name string) (string, bool) {
	clean := filepath.Clean(name)
	if clean == "." || clean == "" || clean == ".." {
		return "", false
	}
	if filepath.Base(clean) != clean {
		return "", false
	}
	return clean, true
}

func cleanCrawlCommitPath(path string) (string, bool) {
	clean := filepath.Clean(path)
	if clean == "." || clean == "" || filepath.IsAbs(clean) {
		return "", false
	}
	return clean, isSafeCrawlCommitPath(clean)
}

func isSafeCrawlCommitPath(path string) bool {
	if path == "" || path == "." || path == ".." || filepath.IsAbs(path) {
		return false
	}
	return !strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func displayCrawlSummary(header string, triage *dungeon.CrawlSummary, inner *dungeon.CrawlSummary) {
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
