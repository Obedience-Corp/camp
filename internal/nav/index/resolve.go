package index

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/obediencecorp/camp/internal/nav"
	"github.com/obediencecorp/camp/internal/nav/fuzzy"
)

// ResolveResult contains the result of path resolution.
type ResolveResult struct {
	// Path is the resolved absolute path.
	Path string
	// Name is the target name.
	Name string
	// Category is the category of the target.
	Category nav.Category
	// Matches contains all matching targets when multiple found.
	Matches []Target
	// Exact indicates if this was an exact match.
	Exact bool
	// Target is the matched target (for accessing shortcuts).
	Target *Target
}

// ResolveOptions configures the resolution behavior.
type ResolveOptions struct {
	// Category limits resolution to a specific category.
	Category nav.Category
	// Query is the search query for fuzzy matching.
	Query string
	// ExactOnly requires an exact name match, not fuzzy.
	ExactOnly bool
	// CampaignRoot is the root directory. Required.
	CampaignRoot string
	// SubShortcut is an optional sub-shortcut within a project target.
	SubShortcut string
}

// InvalidSubShortcutError indicates a sub-shortcut wasn't found.
type InvalidSubShortcutError struct {
	ProjectName    string
	SubShortcut    string
	AvailableNames []string
}

func (e *InvalidSubShortcutError) Error() string {
	return fmt.Sprintf("unknown shortcut '%s' for project '%s'", e.SubShortcut, e.ProjectName)
}

// Resolve finds a navigation target by category and optional query.
// If query is empty, returns the category directory path.
// If query is provided, performs fuzzy search within the category.
func Resolve(ctx context.Context, opts ResolveOptions) (*ResolveResult, error) {
	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if opts.CampaignRoot == "" {
		return nil, fmt.Errorf("campaign root is required")
	}

	// No query - direct category path
	if opts.Query == "" {
		var path string
		if opts.Category == nav.CategoryAll || opts.Category == "" {
			path = opts.CampaignRoot
		} else {
			path = filepath.Join(opts.CampaignRoot, opts.Category.Dir())
		}
		return &ResolveResult{
			Path:     path,
			Category: opts.Category,
			Exact:    true,
		}, nil
	}

	// Has query - use index for search
	return resolveWithQuery(ctx, opts)
}

// resolveWithQuery searches the index for matching targets.
func resolveWithQuery(ctx context.Context, opts ResolveOptions) (*ResolveResult, error) {
	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get or build index
	idx, err := GetOrBuild(ctx, opts.CampaignRoot, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build index: %w", err)
	}

	// Query the index
	query := NewQuery(idx)
	targets := query.ByCategory(opts.Category)

	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets in category %s", opts.Category)
	}

	// First try exact match
	for i := range targets {
		t := &targets[i]
		if t.Name == opts.Query {
			// Handle sub-shortcut if provided
			jumpPath := t.JumpPath(opts.SubShortcut)
			if opts.SubShortcut != "" && jumpPath == "" {
				// Invalid sub-shortcut
				return nil, &InvalidSubShortcutError{
					ProjectName:    t.Name,
					SubShortcut:    opts.SubShortcut,
					AvailableNames: t.ShortcutNames(),
				}
			}
			return &ResolveResult{
				Path:     jumpPath,
				Name:     t.Name,
				Category: t.Category,
				Exact:    true,
				Target:   t,
			}, nil
		}
	}

	// If exact only requested, fail
	if opts.ExactOnly {
		return nil, fmt.Errorf("no exact match for %q in category %s", opts.Query, opts.Category)
	}

	// Fuzzy search
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.Name
	}

	matches := fuzzy.Filter(names, opts.Query)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no targets matching %q in category %s", opts.Query, opts.Category)
	}

	// Build matched targets list
	var matchedTargets []Target
	for _, m := range matches {
		for i := range targets {
			if targets[i].Name == m.Target {
				matchedTargets = append(matchedTargets, targets[i])
				break
			}
		}
	}

	// Single match - return it
	if len(matchedTargets) == 1 {
		t := &matchedTargets[0]
		// Handle sub-shortcut if provided
		jumpPath := t.JumpPath(opts.SubShortcut)
		if opts.SubShortcut != "" && jumpPath == "" {
			// Invalid sub-shortcut
			return nil, &InvalidSubShortcutError{
				ProjectName:    t.Name,
				SubShortcut:    opts.SubShortcut,
				AvailableNames: t.ShortcutNames(),
			}
		}
		return &ResolveResult{
			Path:     jumpPath,
			Name:     t.Name,
			Category: t.Category,
			Matches:  matchedTargets,
			Exact:    false,
			Target:   t,
		}, nil
	}

	// Multiple matches - return all (use first/best match)
	t := &matchedTargets[0]
	// Handle sub-shortcut if provided
	jumpPath := t.JumpPath(opts.SubShortcut)
	if opts.SubShortcut != "" && jumpPath == "" {
		// Invalid sub-shortcut
		return nil, &InvalidSubShortcutError{
			ProjectName:    t.Name,
			SubShortcut:    opts.SubShortcut,
			AvailableNames: t.ShortcutNames(),
		}
	}
	return &ResolveResult{
		Path:     jumpPath,
		Name:     t.Name,
		Category: t.Category,
		Matches:  matchedTargets,
		Exact:    false,
		Target:   t,
	}, nil
}

// ResolvePath is a convenience function that returns just the resolved path.
// Returns empty string and error if resolution fails.
func ResolvePath(ctx context.Context, campaignRoot string, category nav.Category, query string) (string, error) {
	result, err := Resolve(ctx, ResolveOptions{
		CampaignRoot: campaignRoot,
		Category:     category,
		Query:        query,
	})
	if err != nil {
		return "", err
	}
	return result.Path, nil
}

// HasMultipleMatches returns true if resolution found multiple targets.
func (r *ResolveResult) HasMultipleMatches() bool {
	return len(r.Matches) > 1
}

// MatchCount returns the number of matches found.
func (r *ResolveResult) MatchCount() int {
	return len(r.Matches)
}
