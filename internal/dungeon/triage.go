package dungeon

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// RunTriageCrawl performs a triage crawl of the parent directory,
// presenting each item for review with a two-step flow:
// Step 1: Move | Keep | Skip | Quit
// Step 2 (on Move): dynamic status directory picker
func RunTriageCrawl(ctx context.Context, svc *Service, parentPath string) (*CrawlSummary, error) {
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

		// Step 1: high-level decision
		var decision string
		title := fmt.Sprintf("Triage %d/%d: %s", i+1, len(items), item.Name)

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(title).
					Description(infoStr).
					Options(
						huh.NewOption("Keep here - Leave in parent directory", "keep"),
						huh.NewOption("Move - Move to a dungeon status directory", "move"),
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
			return summary, camperrors.Wrap(err, "form error")
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

			if err := svc.MoveToDungeonStatus(ctx, item.Name, parentPath, status); err != nil {
				fmt.Printf("Error moving %s to dungeon/%s: %v\n", item.Name, status, err)
				if hint := moveErrorHint(err); hint != "" {
					fmt.Printf("Hint: %s\n", hint)
				}
				summary.Skipped++
			} else {
				summary.RecordMove(status, item.Name)
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
