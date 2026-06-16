package dungeon

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// CrawlCommitPlan is the domain payload needed to auto-commit crawl moves.
type CrawlCommitPlan struct {
	Description string
	Files       []string
	PreStaged   []string
}

// PrepareCrawlCommit builds the file list and stages tracked source deletions
// for a crawl auto-commit. It returns nil when the summaries contain no moves.
func PrepareCrawlCommit(ctx context.Context, campaignRoot, parentPath, dungeonPath string, triage, inner *CrawlSummary) (*CrawlCommitPlan, error) {
	hasMoves := (triage != nil && triage.HasMoves()) || (inner != nil && inner.HasMoves())
	if !hasMoves {
		return nil, nil
	}

	description := BuildCrawlCommitMessage(campaignRoot, parentPath, triage, inner)

	relDungeon, err := filepath.Rel(campaignRoot, dungeonPath)
	if err != nil {
		relDungeon = dungeonPath
	}

	files := CrawlCommitPaths(relDungeon, triage, inner)
	preStaged, err := stageTrackedCrawlSourceDeletions(
		ctx,
		campaignRoot,
		parentPath,
		relDungeon,
		triage,
		inner,
	)
	if err != nil {
		return nil, camperrors.Wrap(err, "staging crawl source deletions")
	}

	return &CrawlCommitPlan{
		Description: description,
		Files:       files,
		PreStaged:   preStaged,
	}, nil
}

// BuildCrawlCommitMessage builds the commit body listing moved items with paths
// relative to the campaign root.
func BuildCrawlCommitMessage(campaignRoot, parentPath string, triage, inner *CrawlSummary) string {
	relDir, err := filepath.Rel(campaignRoot, parentPath)
	if err != nil {
		relDir = parentPath
	}

	var b strings.Builder

	writeMoves := func(summary *CrawlSummary, prefix string) {
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
			for _, relPath := range items {
				itemName := filepath.Base(relPath)
				fmt.Fprintf(&b, "  - %s/%s\n", relDir, itemName)
			}
			b.WriteString("\n")
		}
	}

	writeMoves(triage, "dungeon")
	writeMoves(inner, "dungeon")

	return strings.TrimRight(b.String(), "\n")
}

// crawlPathSet accumulates unique, safe relative paths for a crawl commit.
// Call append to add a candidate path and sorted to retrieve the deduplicated result.
type crawlPathSet struct {
	seen  map[string]struct{}
	paths []string
}

func newCrawlPathSet() *crawlPathSet {
	return &crawlPathSet{seen: make(map[string]struct{})}
}

// appendSafe cleans path, verifies it is safe, and adds it if not already present.
func (s *crawlPathSet) appendSafe(path string) {
	path = filepath.Clean(path)
	if !isSafeCrawlCommitPath(path) {
		return
	}
	if _, exists := s.seen[path]; exists {
		return
	}
	s.seen[path] = struct{}{}
	s.paths = append(s.paths, path)
}

func (s *crawlPathSet) sorted() []string {
	sort.Strings(s.paths)
	return s.paths
}

// populateMovedPaths appends the destination paths for all moved items in the
// given summaries into ps. MovedItems values are campaign-root-relative
// destination paths stored at move time, so no path reconstruction is needed.
func populateMovedPaths(ps *crawlPathSet, summaries ...*CrawlSummary) {
	for _, summary := range summaries {
		if summary == nil || !summary.HasMoves() {
			continue
		}
		for _, paths := range summary.MovedItems {
			for _, relPath := range paths {
				ps.appendSafe(relPath)
			}
		}
	}
}

// CrawlCommitPaths returns the full set of paths to include in a crawl auto-commit:
// destination paths for moved items, plus the crawl log.
func CrawlCommitPaths(relDungeon string, summaries ...*CrawlSummary) []string {
	ps := newCrawlPathSet()
	populateMovedPaths(ps, summaries...)

	// Always include the crawl log.
	ps.appendSafe(filepath.Join(relDungeon, "crawl.jsonl"))

	return ps.sorted()
}

func stageTrackedCrawlSourceDeletions(
	ctx context.Context,
	campaignRoot string,
	parentPath string,
	relDungeon string,
	triage *CrawlSummary,
	inner *CrawlSummary,
) ([]string, error) {
	sourcePaths := CrawlSourceDeletionPaths(campaignRoot, parentPath, relDungeon, triage, inner)
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

// CrawlSourceDeletionPaths returns the relative source paths that were moved
// and therefore deleted from their origin so they can be staged for removal.
func CrawlSourceDeletionPaths(
	campaignRoot string,
	parentPath string,
	relDungeon string,
	triage *CrawlSummary,
	inner *CrawlSummary,
) []string {
	relParent, err := filepath.Rel(campaignRoot, parentPath)
	if err != nil {
		relParent = parentPath
	}

	ps := newCrawlPathSet()

	appendSummarySourcePaths := func(base string, summary *CrawlSummary) {
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
		for _, paths := range summary.MovedItems {
			for _, relPath := range paths {
				itemName := filepath.Base(relPath)
				cleanName, ok := cleanCrawlCommitName(itemName)
				if !ok {
					continue
				}
				path := cleanName
				if cleanBase != "" {
					path = filepath.Join(cleanBase, cleanName)
				}
				ps.appendSafe(path)
			}
		}
	}

	appendSummarySourcePaths(relParent, triage)
	appendSummarySourcePaths(relDungeon, inner)
	return ps.sorted()
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
