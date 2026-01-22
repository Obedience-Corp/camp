// Package nav provides navigation utilities for campaign directories.
package nav

import (
	"strings"
)

// Category represents a navigation category.
type Category string

const (
	// CategoryProjects is the projects directory.
	CategoryProjects Category = "projects"
	// CategoryWorktrees is the worktrees directory (under projects/).
	CategoryWorktrees Category = "projects/worktrees"
	// CategoryFestivals is the festivals directory.
	CategoryFestivals Category = "festivals"
	// CategoryAIDocs is the AI documentation directory.
	CategoryAIDocs Category = "ai_docs"
	// CategoryDocs is the human documentation directory.
	CategoryDocs Category = "docs"
	// CategoryDungeon is the dungeon directory (archived/paused work).
	CategoryDungeon Category = "dungeon"
	// CategoryWorkflow is the workflow directory.
	CategoryWorkflow Category = "workflow"
	// CategoryCodeReviews is the code reviews directory (under workflow/).
	CategoryCodeReviews Category = "workflow/code_reviews"
	// CategoryPipelines is the pipelines directory (under workflow/).
	CategoryPipelines Category = "workflow/pipelines"
	// CategoryDesign is the design directory (under workflow/).
	CategoryDesign Category = "workflow/design"
	// CategoryIntents is the intents directory (under workflow/).
	CategoryIntents Category = "workflow/intents"
	// CategoryAll represents all directories (no specific category).
	CategoryAll Category = ""
)

// DefaultShortcuts maps single/double letters to categories.
// Single letters are the most common directories.
// Double letters are used when single letters would conflict.
//
// NOTE: This map is only used by ShortcutForCategory() for reverse lookups.
// All runtime shortcut resolution uses shortcuts from campaign.yaml.
// New campaigns get these defaults scaffolded via config.DefaultNavigationShortcuts().
var DefaultShortcuts = map[string]Category{
	"p":  CategoryProjects,    // p = projects
	"pw": CategoryWorktrees,   // pw = project worktrees
	"f":  CategoryFestivals,   // f = festivals
	"a":  CategoryAIDocs,      // a = ai_docs
	"d":  CategoryDocs,        // d = docs
	"du": CategoryDungeon,     // du = dungeon
	"w":  CategoryWorkflow,    // w = workflow
	"cr": CategoryCodeReviews, // cr = code_reviews
	"pi": CategoryPipelines,   // pi = pipelines
	"de": CategoryDesign,      // de = design
	"i":  CategoryIntents,     // i = intents
}

// ParseResult contains the parsed shortcut and remaining query.
type ParseResult struct {
	// Category is the resolved category (empty for all).
	Category Category
	// Query is the remaining search query after the shortcut.
	Query string
	// IsShortcut indicates if a shortcut was matched.
	IsShortcut bool
}

// ParseShortcut parses arguments into category and query.
// The first argument is checked against the provided shortcuts map.
// Only shortcuts from campaign.yaml (via customMappings) are used.
func ParseShortcut(args []string, customMappings map[string]Category) ParseResult {
	if len(args) == 0 {
		return ParseResult{Category: CategoryAll, Query: "", IsShortcut: false}
	}

	first := strings.ToLower(args[0])

	// Check for shortcut match in custom mappings only
	if cat, ok := customMappings[first]; ok {
		query := ""
		if len(args) > 1 {
			query = strings.Join(args[1:], " ")
		}
		return ParseResult{Category: cat, Query: query, IsShortcut: true}
	}

	// No shortcut matched, treat entire input as query
	return ParseResult{
		Category:   CategoryAll,
		Query:      strings.Join(args, " "),
		IsShortcut: false,
	}
}

// Dir returns the directory name for this category.
func (c Category) Dir() string {
	return string(c)
}

// String returns the string representation of the category.
func (c Category) String() string {
	if c == CategoryAll {
		return "all"
	}
	return string(c)
}

// ValidCategories returns all valid category values.
func ValidCategories() []Category {
	return []Category{
		CategoryProjects,
		CategoryWorktrees,
		CategoryFestivals,
		CategoryAIDocs,
		CategoryDocs,
		CategoryDungeon,
		CategoryWorkflow,
		CategoryCodeReviews,
		CategoryPipelines,
		CategoryDesign,
		CategoryIntents,
	}
}

// ShortcutForCategory returns the default shortcut for a category.
func ShortcutForCategory(cat Category) string {
	for shortcut, c := range DefaultShortcuts {
		if c == cat {
			return shortcut
		}
	}
	return ""
}

// MergeShortcuts returns a copy of the custom mappings.
// Only shortcuts from campaign.yaml are used - no hardcoded defaults.
// Deprecated: This function exists for backward compatibility.
// Use the customMappings directly instead.
func MergeShortcuts(customMappings map[string]Category) map[string]Category {
	merged := make(map[string]Category)
	for k, v := range customMappings {
		merged[k] = v
	}
	return merged
}
