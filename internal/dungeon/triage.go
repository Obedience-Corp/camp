package dungeon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/Obedience-Corp/camp/internal/crawl"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// actionDocs is the dungeon-specific extension action for routing
// items to a campaign-root docs subdirectory. It is not a generic
// crawl action; it is recognized only by the triage flow.
const actionDocs crawl.Action = "docs"

func triageActionOptions() []crawl.Option {
	return []crawl.Option{
		{Label: "Keep here - Leave in parent directory", Action: crawl.ActionKeep},
		{Label: "Move - Move to a dungeon status directory", Action: crawl.ActionMove},
		{Label: "Route to docs - Move to campaign docs subdirectory", Action: actionDocs},
		{Label: "Skip - Come back to it another time", Action: crawl.ActionSkip},
		{Label: "Quit - Stop crawling", Action: crawl.ActionQuit},
	}
}

// RunTriageCrawl performs a triage crawl of the parent directory,
// presenting each item for review with a two-step flow:
// Step 1: Move | Route to docs | Keep | Skip | Quit
// Step 2 (on Move): dynamic status directory picker
// Step 2 (on Route to docs): hierarchical docs subdirectory browser
func RunTriageCrawl(ctx context.Context, svc *Service, parentPath string) (*CrawlSummary, error) {
	return runTriageCrawlWithPrompt(ctx, svc, parentPath, crawl.NewDefaultPrompt())
}

func runTriageCrawlWithPrompt(ctx context.Context, svc *Service, parentPath string, prompt crawl.Prompt) (*CrawlSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	items, err := svc.ListParentItems(ctx, parentPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "listing parent items")
	}

	if len(items) == 0 {
		return &CrawlSummary{StatusCounts: map[string]int{}, MovedItems: map[string][]string{}}, nil
	}

	statusDirs, err := svc.ListStatusDirs(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "listing status directories")
	}

	gatherer := NewStatsGatherer()
	summary := &CrawlSummary{StatusCounts: map[string]int{}, MovedItems: map[string][]string{}}

	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return summary, camperrors.Wrap(err, "context cancelled")
		}

		stats := gatherer.Gather(ctx, item.Path)
		infoStr := buildInfoString(item, stats)
		title := fmt.Sprintf("Triage %d/%d: %s", i+1, len(items), item.Name)

	itemLoop:
		for {
			action, err := prompt.SelectAction(ctx, crawl.Item{
				ID:          item.Name,
				Title:       title,
				Description: infoStr,
			}, triageActionOptions())
			if err != nil {
				if crawl.IsAborted(err) {
					return summary, ErrCrawlAborted
				}
				return summary, camperrors.Wrap(err, "first-step prompt")
			}

			switch action {
			case crawl.ActionQuit:
				return summary, nil

			case crawl.ActionMove:
				status, err := promptStatusSelection(ctx, prompt, item.Name, statusDirs)
				if err != nil {
					if crawl.IsAborted(err) || errors.Is(err, ErrCrawlAborted) {
						return summary, ErrCrawlAborted
					}
					fmt.Printf("Error: %v\n", err)
					summary.Skipped++
					break itemLoop
				}
				if status == "" {
					continue itemLoop
				}

				if dstPath, err := svc.MoveToDungeonStatus(ctx, item.Name, parentPath, status); err != nil {
					fmt.Printf("Error moving %s to dungeon/%s: %v\n", item.Name, status, err)
					if hint := moveErrorHint(err); hint != "" {
						fmt.Printf("Hint: %s\n", hint)
					}
					summary.Skipped++
				} else {
					relDst, relErr := filepath.Rel(svc.campaignRoot, dstPath)
					if relErr != nil {
						fmt.Printf("Warning: could not resolve relative path for %s: %v\n", dstPath, relErr)
						relDst = item.Name
					}
					summary.RecordMove(status, relDst)
					if err := logDecision(ctx, svc, item, MoveDecision(status), stats); err != nil {
						fmt.Printf("Warning: failed to log decision: %v\n", err)
					}
				}
				break itemLoop

			case actionDocs:
				destination, err := promptDocsDestination(ctx, item.Name, svc.campaignRoot)
				if err != nil {
					if errors.Is(err, ErrCrawlAborted) {
						return summary, err
					}
					fmt.Printf("Error: %v\n", err)
					summary.Skipped++
					break itemLoop
				}
				if destination == "" {
					continue itemLoop
				}

				targetPath, err := svc.MoveToDocs(ctx, item.Name, parentPath, destination)
				if err != nil {
					fmt.Printf("Error routing %s to docs/%s: %v\n", item.Name, destination, err)
					if hint := moveErrorHint(err); hint != "" {
						fmt.Printf("Hint: %s\n", hint)
					}
					summary.Skipped++
				} else {
					destinationKey := docsMoveSummaryKey(svc.campaignRoot, targetPath)
					relDst, relErr := filepath.Rel(svc.campaignRoot, targetPath)
					if relErr != nil {
						fmt.Printf("Warning: could not resolve relative path for %s: %v\n", targetPath, relErr)
						relDst = item.Name
					}
					summary.RecordMove(destinationKey, relDst)
					if err := logDecision(ctx, svc, item, MoveDecision(destinationKey), stats); err != nil {
						fmt.Printf("Warning: failed to log decision: %v\n", err)
					}
				}
				break itemLoop

			case crawl.ActionKeep:
				summary.Kept++
				if err := logDecision(ctx, svc, item, DecisionKeep, stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
				break itemLoop

			case crawl.ActionSkip:
				summary.Skipped++
				if err := logDecision(ctx, svc, item, DecisionSkip, stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
				break itemLoop
			}
		}
	}

	return summary, nil
}

// promptDocsDestination presents a level-aware docs browser for selecting a
// docs/ subdirectory. Enter selects the current directory. Right/l drills into
// children. Escape goes back up one level, and Escape at root returns "".
func promptDocsDestination(ctx context.Context, itemName string, campaignRoot string) (string, error) {
	return runDocsBrowser(ctx, itemName, campaignRoot)
}

func listDocsSubdirectories(campaignRoot string) ([]string, error) {
	docsRoot := filepath.Join(campaignRoot, docsDirName)
	docsRootInfo, err := os.Stat(docsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, camperrors.Wrap(
				ErrInvalidDocsDestination,
				"campaign-root docs/ directory does not exist",
			)
		}
		return nil, camperrors.Wrap(err, "reading docs root")
	}
	if !docsRootInfo.IsDir() {
		return nil, camperrors.Wrap(
			ErrInvalidDocsDestination,
			"campaign-root docs/ path is not a directory",
		)
	}

	var dirs []string
	err = filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() || path == docsRoot {
			return nil
		}

		rel, err := filepath.Rel(docsRoot, path)
		if err != nil || rel == "." {
			return nil
		}
		dirs = append(dirs, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, camperrors.Wrap(err, "walking docs directories")
	}

	sort.Strings(dirs)
	return dirs, nil
}

func docsMoveSummaryKey(campaignRoot, targetPath string) string {
	docsRoot := filepath.Join(campaignRoot, docsDirName)
	docsDir := filepath.Dir(targetPath)
	rel, err := filepath.Rel(docsRoot, docsDir)
	if err != nil || rel == "." {
		return "docs"
	}
	return "docs/" + filepath.ToSlash(rel)
}
