// Package complete provides completion candidate generation for shell integration.
package complete

import (
	"context"
	"strings"
	"time"

	"github.com/obediencecorp/camp/internal/nav"
	"github.com/obediencecorp/camp/internal/nav/index"
)

// Timeout is the maximum time to spend generating completions.
// Shell completion should be fast to feel responsive.
const Timeout = 50 * time.Millisecond

// Generate returns completion candidates for the given args.
// It uses a timeout to ensure shell responsiveness.
func Generate(ctx context.Context, args []string) ([]string, error) {
	// Add timeout to prevent blocking shell
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	// No args - complete category shortcuts
	if len(args) == 0 {
		return CategoryShortcuts(), nil
	}

	// Check if first arg is a category shortcut
	result := nav.ParseShortcut(args, nil)
	if result.IsShortcut {
		// Complete within category
		return completeInCategory(ctx, result.Category, result.Query)
	}

	// Not a shortcut - complete from all targets and shortcuts
	return completeAll(ctx, args[0])
}

// CategoryShortcuts returns all category shortcut keys.
func CategoryShortcuts() []string {
	return []string{
		"p",  // projects
		"c",  // corpus
		"f",  // festivals
		"a",  // ai_docs
		"d",  // docs
		"w",  // worktrees
		"r",  // code_reviews
		"pi", // pipelines
	}
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
func completeAll(ctx context.Context, query string) ([]string, error) {
	var candidates []string

	// Add matching category shortcuts first
	for _, shortcut := range CategoryShortcuts() {
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
