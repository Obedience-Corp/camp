package dungeon

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// RunTriageCrawl performs a triage crawl of the parent directory,
// presenting each item for review with a two-step flow:
// Step 1: Move | Route to docs | Keep | Skip | Quit
// Step 2 (on Move): dynamic status directory picker
// Step 2 (on Route to docs): hierarchical docs subdirectory browser
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

	itemLoop:
		for {
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
					// Escape at Step 1 = skip this item, not quit entire crawl
					summary.Skipped++
					break itemLoop
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
					break itemLoop
				}
				if status == "" {
					// Cancelled step 2 = go back to Step 1 for same item
					continue itemLoop
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
				break itemLoop

			case "docs":
				destination, err := promptDocsDestination(ctx, item.Name, svc.campaignRoot)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
					summary.Skipped++
					break itemLoop
				}
				if destination == "" {
					// Cancelled docs browser = go back to Step 1 for same item
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
					summary.RecordMove(destinationKey, item.Name)
					if err := logDecision(ctx, svc, item, MoveDecision(destinationKey), stats); err != nil {
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

// promptDocsDestination presents a level-aware tab-completion browser for
// selecting a docs/ subdirectory. Tab cycles through entries at the current
// level only. Selecting a directory with children (trailing "/") drills down.
// Escape goes back up one level. Cancel at root returns "".
func promptDocsDestination(ctx context.Context, itemName string, campaignRoot string) (string, error) {
	allDirs, err := listDocsSubdirectories(campaignRoot)
	if err != nil {
		return "", camperrors.Wrap(err, "listing docs subdirectories")
	}

	// Build a set for final validation (accepts any known subdirectory)
	valid := make(map[string]bool, len(allDirs))
	for _, s := range allDirs {
		valid[s] = true
	}

	docsRoot := filepath.Join(campaignRoot, docsDirName)

	// Custom keymap: Tab = accept suggestion, Enter = submit
	km := huh.NewDefaultKeyMap()
	km.Input.AcceptSuggestion = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete"))
	km.Input.Next = key.NewBinding(key.WithKeys("enter"))

	prefix := ""
	for {
		suggestions := docsLevelSuggestions(docsRoot, prefix)

		// If we've drilled into a leaf directory (no child dirs), select it
		if len(suggestions) == 0 && prefix != "" {
			return strings.TrimSuffix(prefix, "/"), nil
		}

		destination := prefix

		desc := "Tab to complete, Enter to confirm."
		if prefix != "" {
			desc = fmt.Sprintf("Browsing docs/%s — Tab to complete, Esc to go back.", prefix)
		}

		input := huh.NewInput().
			Title(fmt.Sprintf("Route %s to docs/ subdirectory:", itemName)).
			Description(desc).
			Value(&destination)

		if len(suggestions) > 0 {
			input = input.
				Placeholder(suggestions[0]).
				Suggestions(suggestions)
		}

		form := huh.NewForm(huh.NewGroup(input)).WithKeyMap(km)
		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				if prefix == "" {
					return "", nil // Cancel at root = back to Step 1
				}
				prefix = docsParentPrefix(prefix)
				continue // Go up one level
			}
			return "", camperrors.Wrap(err, "form error")
		}

		if destination == "" {
			continue // empty input, re-prompt
		}

		// User hit Enter without changing the pre-filled prefix = select current dir
		if destination == prefix && prefix != "" {
			return strings.TrimSuffix(prefix, "/"), nil
		}

		// Drill down into a directory with children
		if strings.HasSuffix(destination, "/") {
			prefix = destination
			continue
		}

		// Final selection of a leaf directory
		if valid[destination] {
			return destination, nil
		}

		fmt.Printf("Invalid destination %q — must match an existing docs/ subdirectory.\n", destination)
	}
}

// docsLevelSuggestions returns tab-completion suggestions for the current
// directory level. Each suggestion is the full path (prefix + child name).
// Directories with their own subdirectories get a trailing "/".
func docsLevelSuggestions(docsRoot, prefix string) []string {
	dir := docsRoot
	if prefix != "" {
		dir = filepath.Join(docsRoot, strings.TrimSuffix(prefix, "/"))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var suggestions []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fullPath := prefix + e.Name()

		// Check if child has its own subdirectories
		childPath := filepath.Join(docsRoot, fullPath)
		childEntries, _ := os.ReadDir(childPath)
		hasChildren := false
		for _, ce := range childEntries {
			if ce.IsDir() {
				hasChildren = true
				break
			}
		}

		if hasChildren {
			suggestions = append(suggestions, fullPath+"/")
		} else {
			suggestions = append(suggestions, fullPath)
		}
	}

	sort.Strings(suggestions)
	return suggestions
}

// docsParentPrefix returns the parent prefix for back-navigation.
// "architecture/"           → ""
// "business/case-studies/"  → "business/"
// ""                        → ""
func docsParentPrefix(prefix string) string {
	trimmed := strings.TrimSuffix(prefix, "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return ""
	}
	return trimmed[:idx+1]
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
