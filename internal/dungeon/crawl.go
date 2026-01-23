package dungeon

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/obediencecorp/camp/internal/ui/theme"
)

// RunCrawl executes the interactive crawl TUI.
func RunCrawl(ctx context.Context, svc *Service) (*CrawlSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// List items to review
	items, err := svc.ListItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing items: %w", err)
	}

	if len(items) == 0 {
		return &CrawlSummary{}, nil
	}

	// Initialize stats gatherer
	gatherer := NewStatsGatherer()

	summary := &CrawlSummary{}

	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return summary, fmt.Errorf("context cancelled: %w", err)
		}

		// Gather stats for this item
		stats := gatherer.Gather(ctx, item.Path)

		// Build info string
		infoStr := buildInfoString(item, stats)

		// Show selection form
		var decision string
		title := fmt.Sprintf("Item %d/%d: %s", i+1, len(items), item.Name)

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(title).
					Description(infoStr).
					Options(
						huh.NewOption("Keep - Leave in dungeon for later", "keep"),
						huh.NewOption("Archive - Move to archived/ (truly out of the way)", "archive"),
						huh.NewOption("Skip - Come back to it another time", "skip"),
						huh.NewOption("Quit - Stop crawling", "quit"),
					).
					Value(&decision),
			),
		)

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				// Ctrl+C during form - treat as quit
				return summary, nil
			}
			return summary, fmt.Errorf("form error: %w", err)
		}

		// Handle decision
		switch decision {
		case "quit":
			return summary, nil

		case "keep":
			summary.Kept++
			if err := logDecision(ctx, svc, item, DecisionKeep, stats); err != nil {
				// Log errors are non-fatal, just continue
				fmt.Printf("Warning: failed to log decision: %v\n", err)
			}

		case "archive":
			if err := svc.Archive(ctx, item.Name); err != nil {
				fmt.Printf("Error archiving %s: %v\n", item.Name, err)
				summary.Skipped++ // Count as skipped on error
			} else {
				summary.Archived++
				if err := logDecision(ctx, svc, item, DecisionArchive, stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
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
