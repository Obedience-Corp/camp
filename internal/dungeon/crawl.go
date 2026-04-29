package dungeon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Obedience-Corp/camp/internal/crawl"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// statusDirsToOptions converts dungeon StatusDir entries into the
// generic crawl.Option shape consumed by the shared destination
// picker. The Action is always ActionMove; Target is the directory
// name; Count is the directory's item count.
func statusDirsToOptions(dirs []StatusDir) []crawl.Option {
	opts := make([]crawl.Option, 0, len(dirs))
	for _, dir := range dirs {
		opts = append(opts, crawl.Option{
			Label:  dir.Name + "/",
			Action: crawl.ActionMove,
			Target: dir.Name,
			Count:  dir.ItemCount,
		})
	}
	return opts
}

// promptStatusSelection presents the second-step status picker
// using the shared crawl prompt. Returns the chosen status name,
// or empty string if the user backed out (esc).
func promptStatusSelection(ctx context.Context, prompt crawl.Prompt, itemName string, dirs []StatusDir) (string, error) {
	if len(dirs) == 0 {
		return "", fmt.Errorf("no status directories found. Run 'camp dungeon init' to create defaults")
	}
	chosen, err := prompt.SelectDestination(ctx, crawl.Item{ID: itemName, Title: itemName}, statusDirsToOptions(dirs))
	if err != nil {
		return "", err
	}
	return chosen.Target, nil
}

// crawlActionOptions returns the inner-mode first-step options.
func crawlActionOptions() []crawl.Option {
	return []crawl.Option{
		{Label: "Keep - Leave in dungeon for later", Action: crawl.ActionKeep},
		{Label: "Move - Move to a status directory", Action: crawl.ActionMove},
		{Label: "Skip - Come back to it another time", Action: crawl.ActionSkip},
		{Label: "Quit - Stop crawling", Action: crawl.ActionQuit},
	}
}

// RunCrawl executes the interactive crawl TUI for items inside the dungeon.
func RunCrawl(ctx context.Context, svc *Service) (*CrawlSummary, error) {
	return runCrawlWithPrompt(ctx, svc, crawl.NewDefaultPrompt())
}

func runCrawlWithPrompt(ctx context.Context, svc *Service, prompt crawl.Prompt) (*CrawlSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	items, err := svc.ListItems(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "listing items")
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
		title := fmt.Sprintf("Item %d/%d: %s", i+1, len(items), item.Name)

	itemLoop:
		for {
			action, err := prompt.SelectAction(ctx, crawl.Item{
				ID:          item.Name,
				Title:       title,
				Description: infoStr,
			}, crawlActionOptions())
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

				if dstPath, err := svc.MoveToStatus(ctx, item.Name, status); err != nil {
					fmt.Printf("Error moving %s to %s: %v\n", item.Name, status, err)
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

// buildInfoString creates a human-readable info string for display.
func buildInfoString(item DungeonItem, stats *ItemStats) string {
	info := fmt.Sprintf("Type: %s | Modified: %s", item.Type, item.ModTime.Format("2006-01-02"))

	if stats != nil {
		if stats.Files > 0 {
			info += fmt.Sprintf(" | Files: %d", stats.Files)
		}
		if stats.Code > 0 {
			info += fmt.Sprintf(" | Code: %d lines", stats.Code)
		} else if stats.Lines > 0 {
			info += fmt.Sprintf(" | Lines: %d", stats.Lines)
		}
		if stats.Tokens > 0 {
			info += fmt.Sprintf(" | Tokens: %d", stats.Tokens)
		}
	}

	return info
}

// logDecision appends an entry to the crawl log.
func logDecision(ctx context.Context, svc *Service, item DungeonItem, decision Decision, stats *ItemStats) error {
	entry := CrawlEntry{
		Timestamp: time.Now(),
		Item:      item.Name,
		Decision:  decision,
		Info:      stats,
	}
	return svc.AppendCrawlLog(ctx, entry)
}

func moveErrorHint(err error) string {
	switch {
	case errors.Is(err, ErrAlreadyExists):
		return "Destination already contains this item. Choose a different status or rename the item."
	case errors.Is(err, ErrInvalidStatus):
		return "Status must be a single directory name (for example: completed, archived, someday)."
	case errors.Is(err, ErrInvalidDocsDestination):
		return "Docs destination must be an existing subdirectory under campaign-root docs/ (for example: architecture/api)."
	case errors.Is(err, ErrInvalidItemPath):
		return "Item must be a direct child name in the current context (no path separators or traversal)."
	case errors.Is(err, ErrNotFound):
		return "Item no longer exists in the expected source location. Refresh the crawl and retry."
	default:
		return ""
	}
}
