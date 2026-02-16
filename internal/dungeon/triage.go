package dungeon

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/obediencecorp/camp/internal/ui/theme"
)

// RunTriageCrawl performs a triage crawl of the parent directory,
// presenting each item for review with options to move it to the dungeon,
// keep it in place, or skip the decision.
func RunTriageCrawl(ctx context.Context, svc *Service, parentPath string) (*TriageSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	items, err := svc.ListParentItems(ctx, parentPath)
	if err != nil {
		return nil, fmt.Errorf("listing parent items: %w", err)
	}

	if len(items) == 0 {
		return &TriageSummary{}, nil
	}

	gatherer := NewStatsGatherer()
	summary := &TriageSummary{}

	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return summary, fmt.Errorf("context cancelled: %w", err)
		}

		stats := gatherer.Gather(ctx, item.Path)
		infoStr := buildInfoString(item, stats)

		var decision string
		title := fmt.Sprintf("Triage %d/%d: %s", i+1, len(items), item.Name)

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(title).
					Description(infoStr).
					Options(
						huh.NewOption("Move to dungeon - Unsorted, review later", string(DecisionMoveToDungeon)),
						huh.NewOption("Move to completed - Directly into completed/", string(DecisionCompleted)),
						huh.NewOption("Move to archived - Directly into archived/", string(DecisionArchive)),
						huh.NewOption("Move to someday - Directly into someday/", string(DecisionSomeday)),
						huh.NewOption("Keep here - Leave in parent directory", string(DecisionKeep)),
						huh.NewOption("Skip - Come back to it another time", string(DecisionSkip)),
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

		case string(DecisionMoveToDungeon):
			if err := svc.MoveToDungeon(ctx, item.Name, parentPath); err != nil {
				fmt.Printf("Error moving %s to dungeon: %v\n", item.Name, err)
				summary.Skipped++
			} else {
				summary.Moved++
				if err := logDecision(ctx, svc, item, DecisionMoveToDungeon, stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
			}

		case string(DecisionCompleted):
			if err := svc.MoveToDungeonStatus(ctx, item.Name, parentPath, "completed"); err != nil {
				fmt.Printf("Error moving %s to completed: %v\n", item.Name, err)
				summary.Skipped++
			} else {
				summary.Completed++
				if err := logDecision(ctx, svc, item, DecisionCompleted, stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
			}

		case string(DecisionArchive):
			if err := svc.MoveToDungeonStatus(ctx, item.Name, parentPath, "archived"); err != nil {
				fmt.Printf("Error moving %s to archived: %v\n", item.Name, err)
				summary.Skipped++
			} else {
				summary.Archived++
				if err := logDecision(ctx, svc, item, DecisionArchive, stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
			}

		case string(DecisionSomeday):
			if err := svc.MoveToDungeonStatus(ctx, item.Name, parentPath, "someday"); err != nil {
				fmt.Printf("Error moving %s to someday: %v\n", item.Name, err)
				summary.Skipped++
			} else {
				summary.Someday++
				if err := logDecision(ctx, svc, item, DecisionSomeday, stats); err != nil {
					fmt.Printf("Warning: failed to log decision: %v\n", err)
				}
			}

		case string(DecisionKeep):
			summary.Kept++
			if err := logDecision(ctx, svc, item, DecisionKeep, stats); err != nil {
				fmt.Printf("Warning: failed to log decision: %v\n", err)
			}

		case string(DecisionSkip):
			summary.Skipped++
			if err := logDecision(ctx, svc, item, DecisionSkip, stats); err != nil {
				fmt.Printf("Warning: failed to log decision: %v\n", err)
			}
		}
	}

	return summary, nil
}
