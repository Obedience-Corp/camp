package dungeon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// promptStatusSelection presents a second-step selector showing available status
// directories with item counts. Returns the chosen status name, or empty string
// if the user goes back to the previous crawl menu.
func promptStatusSelection(ctx context.Context, itemName string, dirs []StatusDir) (string, error) {
	return runStatusPicker(ctx, itemName, dirs)
}

// RunCrawl executes the interactive crawl TUI for items inside the dungeon.
func RunCrawl(ctx context.Context, svc *Service) (*CrawlSummary, error) {
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

	// Fetch status dirs once before the loop
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

	itemLoop:
		for {
			// Step 1: high-level decision
			var decision string
			title := fmt.Sprintf("Item %d/%d: %s", i+1, len(items), item.Name)

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title(title).
						Description(infoStr).
						Options(
							huh.NewOption("Keep - Leave in dungeon for later", "keep"),
							huh.NewOption("Move - Move to a status directory", "move"),
							huh.NewOption("Skip - Come back to it another time", "skip"),
							huh.NewOption("Quit - Stop crawling", "quit"),
						).
						Value(&decision),
				),
			)

			if err := theme.RunForm(ctx, form); err != nil {
				if theme.IsCancelled(err) {
					return summary, ErrCrawlAborted
				}
				return summary, camperrors.Wrap(err, "form error")
			}

			switch decision {
			case "quit":
				return summary, nil

			case "move":
				status, err := promptStatusSelection(ctx, item.Name, statusDirs)
				if err != nil {
					if errors.Is(err, ErrCrawlAborted) {
						return summary, err
					}
					fmt.Printf("Error: %v\n", err)
					summary.Skipped++
					break itemLoop
				}
				if status == "" {
					// Cancelled step 2 = go back to Step 1 for same item
					continue itemLoop
				}

				if dstPath, err := svc.MoveToStatus(ctx, item.Name, status); err != nil {
					fmt.Printf("Error moving %s to %s: %v\n", item.Name, status, err)
					if hint := moveErrorHint(err); hint != "" {
						fmt.Printf("Hint: %s\n", hint)
					}
					summary.Skipped++
				} else {
					// Safe: dstPath is always under campaignRoot (constructed from dungeonPath).
					relDst, relErr := filepath.Rel(svc.campaignRoot, dstPath)
					if relErr != nil {
						fmt.Printf("Warning: could not resolve relative path for %s: %v\n", dstPath, relErr)
						relDst = item.Name // fallback to item name for commit
					}
					summary.RecordMove(status, relDst)
					if err := logDecision(ctx, svc, item, MoveDecision(status), stats); err != nil {
						fmt.Printf("Warning: failed to log decision: %v\n", err)
					}
				}
				break itemLoop

			case "keep":
				summary.Kept++
				if err := logDecision(ctx, svc, item, DecisionKeep, stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
				break itemLoop

			case "skip":
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
