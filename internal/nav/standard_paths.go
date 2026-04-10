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
	".campaign/intents/":     CategoryIntents,
	".campaign/intents":      CategoryIntents,
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

// BuildCategoryMappings converts config shortcuts and an optional raw paths
// map to nav.Category mappings. Explicit shortcuts take precedence; path
// concept names (e.g. "docs", "ai_docs", "festivals") fill in the gaps so
// that both `cgo d` and `cgo docs` resolve correctly.
//
// pathsMap is the raw concept-name→directory map from jumps.yaml (via
// CampaignConfig.PathsMap()). Pass nil to skip concept-name mappings.
func BuildCategoryMappings(shortcuts map[string]config.ShortcutConfig, pathsMap map[string]string) map[string]Category {
	mappings := make(map[string]Category)

	// First, add concept-name mappings from the paths config so that
	// full directory names work as navigation targets.
	for name, path := range pathsMap {
		if cat, ok := CategoryForStandardPath(path); ok {
			mappings[name] = cat
		}
	}

	// Then layer explicit shortcuts on top (they win on collision).
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
