package nav

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
)

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
	"workflow/reviews/":      CategoryReviews,
	"workflow/reviews":       CategoryReviews,
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

// FestivalStatusDirs are dest buckets that hold individual festival directories.
// Live statuses are listed first so nested fuzzy search prefers an active
// festival over a planning copy of the same name.
var FestivalStatusDirs = []string{"active", "ready", "planning", "ritual", "chains"}

// IsFestivalStatusDir reports whether name is a festival dest bucket.
func IsFestivalStatusDir(name string) bool {
	normalized := NormalizeNavigationName(name)
	for _, dir := range FestivalStatusDirs {
		if normalized == dir {
			return true
		}
	}
	return false
}

// IsFestivalsRelativePath reports whether relativePath is the festivals root.
func IsFestivalsRelativePath(relativePath string) bool {
	cleaned := strings.Trim(strings.TrimSpace(relativePath), "/")
	return cleaned == string(CategoryFestivals)
}

// BuildCategoryMappings converts configured shortcut keys to nav.Category
// mappings. Only explicit navigation shortcuts are included here. Long-form
// directory aliases and configured concepts are resolved separately so those
// layers do not get conflated with shortcut keys.
func BuildCategoryMappings(shortcuts map[string]config.ShortcutConfig) map[string]Category {
	mappings := make(map[string]Category)

	for name, sc := range shortcuts {
		if !sc.IsNavigation() {
			continue
		}
		if cat, ok := CategoryForStandardPath(sc.Path); ok {
			mappings[NormalizeNavigationName(name)] = cat
		}
	}
	return mappings
}
