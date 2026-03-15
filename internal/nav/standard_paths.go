package nav

import "github.com/Obedience-Corp/camp/internal/config"

var standardPathCategories = map[string]Category{
	"projects/":              CategoryProjects,
	"projects":               CategoryProjects,
	"projects/worktrees/":    CategoryWorktrees,
	"projects/worktrees":     CategoryWorktrees,
	"festivals/":             CategoryFestivals,
	"festivals":              CategoryFestivals,
	"ai_docs/":               CategoryAIDocs,
	"ai_docs":                CategoryAIDocs,
	"docs/":                  CategoryDocs,
	"docs":                   CategoryDocs,
	"dungeon/":               CategoryDungeon,
	"dungeon":                CategoryDungeon,
	"workflow/":              CategoryWorkflow,
	"workflow":               CategoryWorkflow,
	"workflow/code_reviews/": CategoryCodeReviews,
	"workflow/code_reviews":  CategoryCodeReviews,
	"workflow/pipelines/":    CategoryPipelines,
	"workflow/pipelines":     CategoryPipelines,
	"workflow/design/":       CategoryDesign,
	"workflow/design":        CategoryDesign,
	"workflow/intents/":      CategoryIntents,
	"workflow/intents":       CategoryIntents,
}

// CategoryForStandardPath resolves a well-known navigation path to a category.
func CategoryForStandardPath(path string) (Category, bool) {
	cat, ok := standardPathCategories[path]
	return cat, ok
}

// IsStandardPath reports whether a path maps to a built-in navigation category.
func IsStandardPath(path string) bool {
	_, ok := CategoryForStandardPath(path)
	return ok
}

// BuildCategoryMappings converts config shortcuts to nav.Category mappings.
func BuildCategoryMappings(shortcuts map[string]config.ShortcutConfig) map[string]Category {
	mappings := make(map[string]Category)
	for name, sc := range shortcuts {
		if !sc.IsNavigation() {
			continue
		}
		if cat, ok := CategoryForStandardPath(sc.Path); ok {
			mappings[name] = cat
		}
	}
	return mappings
}
