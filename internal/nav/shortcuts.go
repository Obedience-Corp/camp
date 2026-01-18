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
	// CategoryCorpus is the corpus/reference materials directory.
	CategoryCorpus Category = "corpus"
	// CategoryFestivals is the festivals directory.
	CategoryFestivals Category = "festivals"
	// CategoryAIDocs is the AI documentation directory.
	CategoryAIDocs Category = "ai_docs"
	// CategoryDocs is the human documentation directory.
	CategoryDocs Category = "docs"
	// CategoryWorktrees is the worktrees directory.
	CategoryWorktrees Category = "worktrees"
	// CategoryCodeReviews is the code reviews directory.
	CategoryCodeReviews Category = "code_reviews"
	// CategoryPipelines is the pipelines directory.
	CategoryPipelines Category = "pipelines"
	// CategoryAll represents all directories (no specific category).
	CategoryAll Category = ""
)

// DefaultShortcuts maps single/double letters to categories.
// Single letters are the most common directories.
// Double letters are used when single letters would conflict.
var DefaultShortcuts = map[string]Category{
	"p":  CategoryProjects,    // p = projects
	"c":  CategoryCorpus,      // c = corpus
	"f":  CategoryFestivals,   // f = festivals
	"a":  CategoryAIDocs,      // a = ai_docs
	"d":  CategoryDocs,        // d = docs
	"w":  CategoryWorktrees,   // w = worktrees
	"r":  CategoryCodeReviews, // r = code_reviews
	"pi": CategoryPipelines,   // pi = pipelines (two letters to avoid conflict)
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
// The first argument is checked against known shortcuts.
// Custom mappings can override or extend default shortcuts.
func ParseShortcut(args []string, customMappings map[string]Category) ParseResult {
	if len(args) == 0 {
		return ParseResult{Category: CategoryAll, Query: "", IsShortcut: false}
	}

	// Build shortcuts map: defaults first, then custom overrides
	shortcuts := make(map[string]Category)
	for k, v := range DefaultShortcuts {
		shortcuts[k] = v
	}
	for k, v := range customMappings {
		shortcuts[k] = v
	}

	first := strings.ToLower(args[0])

	// Check for shortcut match
	if cat, ok := shortcuts[first]; ok {
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
		CategoryCorpus,
		CategoryFestivals,
		CategoryAIDocs,
		CategoryDocs,
		CategoryWorktrees,
		CategoryCodeReviews,
		CategoryPipelines,
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

// MergeShortcuts combines default navigation shortcuts with custom mappings.
// Custom mappings take precedence over defaults.
func MergeShortcuts(customMappings map[string]Category) map[string]Category {
	merged := make(map[string]Category)

	// Start with defaults
	for k, v := range DefaultShortcuts {
		merged[k] = v
	}

	// Override with custom mappings
	for k, v := range customMappings {
		merged[k] = v
	}

	return merged
}
