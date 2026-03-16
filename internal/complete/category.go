package complete

import (
	"context"
	"fmt"
	"sort"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
)

// CategoryCandidate represents a completion candidate with metadata.
type CategoryCandidate struct {
	// Value is the completion value.
	Value string
	// Description is a human-readable description (used by fish).
	Description string
	// Category is the nav category (if applicable).
	Category nav.Category
}

// Categories returns category shortcut candidates with descriptions.
func Categories() []CategoryCandidate {
	return []CategoryCandidate{
		{Value: "p", Description: "projects/", Category: nav.CategoryProjects},
		{Value: "pw", Description: "projects/worktrees/", Category: nav.CategoryWorktrees},
		{Value: "f", Description: "festivals/", Category: nav.CategoryFestivals},
		{Value: "ai", Description: "ai_docs/", Category: nav.CategoryAIDocs},
		{Value: "d", Description: "docs/", Category: nav.CategoryDocs},
		{Value: "du", Description: "dungeon/", Category: nav.CategoryDungeon},
		{Value: "w", Description: "workflow/", Category: nav.CategoryWorkflow},
		{Value: "cr", Description: "workflow/code_reviews/", Category: nav.CategoryCodeReviews},
		{Value: "pi", Description: "workflow/pipelines/", Category: nav.CategoryPipelines},
		{Value: "de", Description: "workflow/design/", Category: nav.CategoryDesign},
		{Value: "i", Description: ".campaign/intents/", Category: nav.CategoryIntents},
		{Value: "t", Description: "toggle (last location)"},
		{Value: "toggle", Description: "jump to last visited location"},
	}
}

// Campaigns returns registered campaign names for completion.
// Results are sorted by last access time (most recent first).
func Campaigns(ctx context.Context) []CategoryCandidate {
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil
	}

	var candidates []CategoryCandidate
	for name, c := range reg.Campaigns {
		candidates = append(candidates, CategoryCandidate{
			Value:       name,
			Description: string(c.Type),
		})
	}

	// Sort by last access time (most recent first)
	sort.Slice(candidates, func(i, j int) bool {
		ci := reg.Campaigns[candidates[i].Value]
		cj := reg.Campaigns[candidates[j].Value]
		return ci.LastAccess.After(cj.LastAccess)
	})

	return candidates
}

// GenerateWithDescriptions returns candidates with descriptions for fish shell.
// It returns category shortcuts and registered campaigns.
func GenerateWithDescriptions(ctx context.Context, args []string) []CategoryCandidate {
	if len(args) == 0 {
		var candidates []CategoryCandidate
		// Add category shortcuts
		candidates = append(candidates, Categories()...)
		// Add campaigns
		candidates = append(candidates, Campaigns(ctx)...)
		return candidates
	}

	// Has args - delegate to normal completion and wrap results
	values, err := Generate(ctx, args)
	if err != nil {
		return nil
	}

	candidates := make([]CategoryCandidate, len(values))
	for i, v := range values {
		candidates[i] = CategoryCandidate{Value: v}
	}
	return candidates
}

// FormatForShell formats candidates for a specific shell.
// Fish shell uses tab-separated value and description.
// Other shells just use the value.
func FormatForShell(candidates []CategoryCandidate, shell string) []string {
	output := make([]string, len(candidates))

	for i, c := range candidates {
		switch shell {
		case "fish":
			// Fish supports tab-separated descriptions
			if c.Description != "" {
				output[i] = fmt.Sprintf("%s\t%s", c.Value, c.Description)
			} else {
				output[i] = c.Value
			}
		default:
			// Others just get values
			output[i] = c.Value
		}
	}

	return output
}

// CategoryByShortcut returns the category for a shortcut.
// Returns CategoryAll if not found.
func CategoryByShortcut(shortcut string) nav.Category {
	for _, c := range Categories() {
		if c.Value == shortcut {
			return c.Category
		}
	}
	return nav.CategoryAll
}

// DescriptionForCategory returns the description for a category shortcut.
func DescriptionForCategory(shortcut string) string {
	for _, c := range Categories() {
		if c.Value == shortcut {
			return c.Description
		}
	}
	return ""
}
