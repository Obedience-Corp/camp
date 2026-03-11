package dungeon

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// RunTriageCrawl performs a triage crawl of the parent directory,
// presenting each item for review with a two-step flow:
// Step 1: Move | Route to docs | Keep | Skip | Quit
// Step 2 (on Move): dynamic status directory picker
// Step 2 (on Route to docs): existing docs subdirectory picker with suggestions
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
	docsSuggestions, err := listDocsSubdirectories(svc.campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "listing docs subdirectories")
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
						huh.NewOption("Route to docs - Move to campaign docs subdirectory", "docs"),
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

			if _, err := svc.MoveToDungeonStatus(ctx, item.Name, parentPath, status); err != nil {
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

		case "docs":
			destination, err := promptDocsDestination(ctx, item.Name, docsSuggestions)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				summary.Skipped++
				continue
			}
			if destination == "" {
				// Cancelled step 2 = skip
				summary.Skipped++
				continue
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
				summary.RecordMove(destinationKey, item.Name)
				docsSuggestions = appendDocsSuggestion(docsSuggestions, strings.TrimPrefix(destinationKey, "docs/"))
				if err := logDecision(ctx, svc, item, MoveDecision(destinationKey), stats); err != nil {
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

func promptDocsDestination(ctx context.Context, itemName string, suggestions []string) (string, error) {
	var destination string
	input := huh.NewInput().
		Title(fmt.Sprintf("Route %s to an existing docs/ subdirectory:", itemName)).
		Description("Select or type an existing docs subdirectory (for example: architecture/api).").
		Placeholder("architecture/api").
		Value(&destination)

	if len(suggestions) > 0 {
		input = input.Suggestions(suggestions)
	}

	form := huh.NewForm(huh.NewGroup(input))
	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return "", nil
		}
		return "", camperrors.Wrap(err, "form error")
	}

	destination = strings.TrimSpace(destination)
	if destination == "" {
		return "", nil
	}

	return destination, nil
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

func appendDocsSuggestion(suggestions []string, destination string) []string {
	if destination == "" || destination == "." {
		return suggestions
	}
	for _, existing := range suggestions {
		if existing == destination {
			return suggestions
		}
	}
	suggestions = append(suggestions, destination)
	sort.Strings(suggestions)
	return suggestions
}
