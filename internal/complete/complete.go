// Package complete provides completion candidate generation for shell integration.
package complete

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/nav"
	"github.com/obediencecorp/camp/internal/nav/index"
)

// RichCategoryGroup groups rich completion candidates by category.
type RichCategoryGroup struct {
	Category   string
	Candidates []index.CompletionCandidate
}

// Timeout is the maximum time to spend generating completions.
// 200ms allows room for cold-start index rebuilds while staying responsive.
const Timeout = 200 * time.Millisecond

// Generate returns completion candidates for the given args.
// It uses a timeout to ensure shell responsiveness.
func Generate(ctx context.Context, args []string) ([]string, error) {
	// Add timeout to prevent blocking shell
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	// Load shortcuts from campaign config
	shortcuts := loadShortcutMappings(ctx)

	// No args - complete category shortcuts
	if len(args) == 0 {
		return shortcutKeys(shortcuts), nil
	}

	// Check if first arg is a category shortcut
	result := nav.ParseShortcut(args, shortcuts)
	if result.IsShortcut {
		// Complete within category
		return completeInCategory(ctx, result.Category, result.Query)
	}

	// Not a shortcut - complete from all targets and shortcuts
	return completeAll(ctx, args[0], shortcuts)
}

// GenerateRich returns completion candidates with fuzzy matching and path descriptions.
// Results are grouped by category.
func GenerateRich(ctx context.Context, args []string) ([]RichCategoryGroup, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	// Determine query and category
	var query string
	var cat nav.Category
	if len(args) > 0 {
		query = args[0]
	}

	// Check if first arg is a category shortcut
	shortcuts := loadShortcutMappings(ctx)
	result := nav.ParseShortcut(args, shortcuts)
	if result.IsShortcut {
		cat = result.Category
		query = result.Query
	}

	// Get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		return nil, err
	}

	// Get or build index
	idx, err := index.GetOrBuild(ctx, jumpResult.Path, false)
	if err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get fuzzy completion candidates
	q := index.NewQuery(idx)
	if cat == "" {
		cat = nav.CategoryAll
	}
	candidates := q.FuzzyComplete(query, cat)

	// Group by category
	categoryMap := make(map[string][]index.CompletionCandidate)
	for _, c := range candidates {
		categoryMap[c.Category] = append(categoryMap[c.Category], c)
	}

	grouped := make([]RichCategoryGroup, 0, len(categoryMap))
	for catName, cands := range categoryMap {
		grouped = append(grouped, RichCategoryGroup{
			Category:   catName,
			Candidates: cands,
		})
	}

	sort.Slice(grouped, func(i, j int) bool {
		return grouped[i].Category < grouped[j].Category
	})

	return grouped, nil
}

// loadShortcutMappings loads shortcuts from campaign config.
// Returns empty map if not in a campaign or on error.
func loadShortcutMappings(ctx context.Context) map[string]nav.Category {
	cfg, _, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil
	}
	return buildCategoryMappings(cfg.Shortcuts())
}

// buildCategoryMappings converts config shortcuts to nav.Category mappings.
func buildCategoryMappings(shortcuts map[string]config.ShortcutConfig) map[string]nav.Category {
	// Standard paths that map to categories
	standardPaths := map[string]nav.Category{
		"projects/":              nav.CategoryProjects,
		"projects":               nav.CategoryProjects,
		"projects/worktrees/":    nav.CategoryWorktrees,
		"projects/worktrees":     nav.CategoryWorktrees,
		"festivals/":             nav.CategoryFestivals,
		"festivals":              nav.CategoryFestivals,
		"ai_docs/":               nav.CategoryAIDocs,
		"ai_docs":                nav.CategoryAIDocs,
		"docs/":                  nav.CategoryDocs,
		"docs":                   nav.CategoryDocs,
		"dungeon/":               nav.CategoryDungeon,
		"dungeon":                nav.CategoryDungeon,
		"workflow/":              nav.CategoryWorkflow,
		"workflow":               nav.CategoryWorkflow,
		"workflow/code_reviews/": nav.CategoryCodeReviews,
		"workflow/code_reviews":  nav.CategoryCodeReviews,
		"workflow/pipelines/":    nav.CategoryPipelines,
		"workflow/pipelines":     nav.CategoryPipelines,
		"workflow/design/":       nav.CategoryDesign,
		"workflow/design":        nav.CategoryDesign,
		"workflow/intents/":      nav.CategoryIntents,
		"workflow/intents":       nav.CategoryIntents,
	}

	mappings := make(map[string]nav.Category)
	for name, sc := range shortcuts {
		if sc.IsNavigation() {
			if cat, ok := standardPaths[sc.Path]; ok {
				mappings[name] = cat
			}
		}
	}
	return mappings
}

// shortcutKeys returns the keys from a shortcuts map.
func shortcutKeys(shortcuts map[string]nav.Category) []string {
	if len(shortcuts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(shortcuts))
	for k := range shortcuts {
		keys = append(keys, k)
	}
	return keys
}

// CategoryShortcuts returns category shortcut keys from campaign config.
// Returns nil if not in a campaign.
func CategoryShortcuts() []string {
	ctx := context.Background()
	shortcuts := loadShortcutMappings(ctx)
	return shortcutKeys(shortcuts)
}

// completeInCategory returns completion candidates within a specific category.
func completeInCategory(ctx context.Context, cat nav.Category, query string) ([]string, error) {
	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Special handling for worktrees with @ syntax
	if cat == nav.CategoryWorktrees && (strings.Contains(query, "@") || query != "") {
		return CompleteWorktree(ctx, query)
	}

	// Special handling for festivals with path syntax
	if cat == nav.CategoryFestivals && strings.Contains(query, "/") {
		return CompleteFestival(ctx, query)
	}

	// Get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		return nil, err
	}

	// Special handling for flow paths (contains "/" and first segment is a flow)
	// This enables syntax like: cgo de myflow/active/item
	if strings.Contains(query, "/") {
		if candidates, handled, err := CompleteFlowInCategory(ctx, cat, jumpResult.Path, query); handled {
			return candidates, err
		}
	}

	// Get or build index
	idx, err := index.GetOrBuild(ctx, jumpResult.Path, false)
	if err != nil {
		return nil, err
	}

	// Check context again
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Use index query for completion
	q := index.NewQuery(idx)

	if query == "" {
		// No query - return all targets in category
		targets := q.ByCategory(cat)
		candidates := make([]string, len(targets))
		for i, t := range targets {
			candidates[i] = t.Name
		}
		return candidates, nil
	}

	// Has query - filter by prefix
	candidates := q.Complete(query, cat)
	return candidates, nil
}

// completeAll returns completion candidates from all categories plus shortcuts.
func completeAll(ctx context.Context, query string, shortcuts map[string]nav.Category) ([]string, error) {
	var candidates []string

	// Add matching category shortcuts first
	for shortcut := range shortcuts {
		if strings.HasPrefix(shortcut, query) {
			candidates = append(candidates, shortcut)
		}
	}

	// Check context
	if ctx.Err() != nil {
		return candidates, ctx.Err()
	}

	// Get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		// Return just shortcuts if not in campaign
		return candidates, nil
	}

	// Get or build index
	idx, err := index.GetOrBuild(ctx, jumpResult.Path, false)
	if err != nil {
		return candidates, nil
	}

	// Check context again
	if ctx.Err() != nil {
		return candidates, ctx.Err()
	}

	// Complete from all categories
	q := index.NewQuery(idx)
	targetCandidates := q.CompleteAny(query, nav.CategoryAll)
	candidates = append(candidates, targetCandidates...)

	return candidates, nil
}
