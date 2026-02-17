package dungeon

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/obediencecorp/camp/internal/ui/theme"
)

// promptStatusSelection presents a second-step selector showing available status
// directories with item counts. Returns the chosen status name, or empty string
// if the user cancels (treated as skip).
func promptStatusSelection(ctx context.Context, itemName string, dirs []StatusDir) (string, error) {
	if len(dirs) == 0 {
		return "", fmt.Errorf("no status directories found. Run 'camp dungeon init' to create defaults")
	}

	var opts []huh.Option[string]
	for _, d := range dirs {
		label := fmt.Sprintf("%s/ (%d items)", d.Name, d.ItemCount)
		opts = append(opts, huh.NewOption(label, d.Name))
	}

	var status string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Move %s to:", itemName)).
				Options(opts...).
				Value(&status),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return "", nil // Cancel = skip
		}
		return "", fmt.Errorf("form error: %w", err)
	}

	return status, nil
}

// RunCrawl executes the interactive crawl TUI for items inside the dungeon.
func RunCrawl(ctx context.Context, svc *Service) (*CrawlSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	items, err := svc.ListItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing items: %w", err)
	}

	if len(items) == 0 {
		return &CrawlSummary{StatusCounts: map[string]int{}}, nil
	}

	// Fetch status dirs once before the loop
	statusDirs, err := svc.ListStatusDirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing status directories: %w", err)
	}

	gatherer := NewStatsGatherer()
	summary := &CrawlSummary{StatusCounts: map[string]int{}}

	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return summary, fmt.Errorf("context cancelled: %w", err)
		}

		stats := gatherer.Gather(ctx, item.Path)
		infoStr := buildInfoString(item, stats)

		// Step 1: high-level decision
		var decision string
		title := fmt.Sprintf("Item %d/%d: %s", i+1, len(items), item.Name)

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(title).
					Description(infoStr).
					Options(
						huh.NewOption("Move - Move to a status directory", "move"),
						huh.NewOption("Keep - Leave in dungeon for later", "keep"),
						huh.NewOption("Skip - Come back to it another time", "skip"),
						huh.NewOption("Quit - Stop crawling", "quit"),
					).
					Value(&decision),
			),
		)

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return summary, nil
			}
			return summary, fmt.Errorf("form error: %w", err)
		}

		switch decision {
		case "quit":
			return summary, nil

		case "move":
			status, err := promptStatusSelection(ctx, item.Name, statusDirs)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				summary.Skipped++
				continue
			}
			if status == "" {
				// Cancelled step 2 = skip
				summary.Skipped++
				continue
			}

			if err := svc.MoveToStatus(ctx, item.Name, status); err != nil {
				fmt.Printf("Error moving %s to %s: %v\n", item.Name, status, err)
				summary.Skipped++
			} else {
				summary.StatusCounts[status]++
				if err := logDecision(ctx, svc, item, MoveDecision(status), stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
			}

		case "keep":
			summary.Kept++
			if err := logDecision(ctx, svc, item, DecisionKeep, stats); err != nil {
				fmt.Printf("Warning: failed to log decision: %v\n", err)
			}

		case "skip":
			summary.Skipped++
			if err := logDecision(ctx, svc, item, DecisionSkip, stats); err != nil {
				fmt.Printf("Warning: failed to log decision: %v\n", err)
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
