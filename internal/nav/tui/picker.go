// Package tui provides terminal UI components for navigation.
package tui

import (
	"errors"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/ktr0731/go-fuzzyfinder"
)

// ErrNoTargets is returned when attempting to pick from an empty list.
var ErrNoTargets = errors.New("no targets available")

// ErrAborted is returned when the user cancels selection with Esc.
var ErrAborted = errors.New("selection aborted")

// Target represents a navigation target in the picker.
type Target struct {
	// Name is the display name of the target.
	Name string
	// Path is the absolute path to the target.
	Path string
	// Category is the category this target belongs to.
	Category nav.Category
	// Description provides optional additional context.
	Description string
}

// PickResult contains the result of a picker selection.
type PickResult struct {
	// Target is the selected target.
	Target Target
	// Index is the index of the selected target in the original slice.
	Index int
}

// PickOptions configures the picker behavior.
type PickOptions struct {
	// Query is the initial query to filter targets.
	Query string
	// Prompt is the prompt text shown to the user.
	Prompt string
	// Header is optional text shown at the top of the picker (e.g., keybinding hints).
	Header string
	// ShowPreview enables the preview window.
	ShowPreview bool
}

// DefaultPickOptions returns sensible default options.
func DefaultPickOptions() PickOptions {
	return PickOptions{
		Prompt:      "Navigate to: ",
		ShowPreview: false,
	}
}

// Pick shows the fuzzy picker and returns the selected target.
// Returns ErrAborted if the user presses Esc.
// Returns ErrNoTargets if the targets slice is empty.
func Pick(targets []Target, opts PickOptions) (*PickResult, error) {
	if len(targets) == 0 {
		return nil, ErrNoTargets
	}

	// Build fuzzyfinder options
	finderOpts := []fuzzyfinder.Option{
		fuzzyfinder.WithPromptString(opts.Prompt),
	}

	if opts.Query != "" {
		finderOpts = append(finderOpts, fuzzyfinder.WithQuery(opts.Query))
	}

	if opts.Header != "" {
		finderOpts = append(finderOpts, fuzzyfinder.WithHeader(opts.Header))
	}

	if opts.ShowPreview {
		finderOpts = append(finderOpts, fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i < 0 || i >= len(targets) {
				return ""
			}
			t := targets[i]
			preview := fmt.Sprintf("Path: %s\nCategory: %s", t.Path, t.Category.String())
			if t.Description != "" {
				preview += fmt.Sprintf("\n\n%s", t.Description)
			}
			return preview
		}))
	}

	idx, err := fuzzyfinder.Find(
		targets,
		func(i int) string {
			return formatTarget(targets[i])
		},
		finderOpts...,
	)

	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return nil, ErrAborted
		}
		return nil, fmt.Errorf("picker error: %w", err)
	}

	return &PickResult{
		Target: targets[idx],
		Index:  idx,
	}, nil
}

// PickScoped shows picker scoped to a specific category.
// If category is CategoryAll, all targets are shown.
func PickScoped(targets []Target, category nav.Category, opts PickOptions) (*PickResult, error) {
	// Filter targets to category
	scoped := filterByCategory(targets, category)

	if len(scoped) == 0 {
		return nil, &NoTargetsInCategoryError{Category: category}
	}

	return Pick(scoped, opts)
}

// PickPath is a convenience function that returns just the selected path.
// Returns empty string if user cancels.
func PickPath(targets []Target, query string) (string, error) {
	opts := DefaultPickOptions()
	opts.Query = query

	result, err := Pick(targets, opts)
	if err != nil {
		if errors.Is(err, ErrAborted) {
			return "", nil
		}
		return "", err
	}

	return result.Target.Path, nil
}

// formatTarget formats a target for display in the picker.
func formatTarget(t Target) string {
	if t.Category != nav.CategoryAll && t.Category != "" {
		return string(t.Category) + "/" + t.Name
	}
	return t.Name
}

// filterByCategory returns targets matching the given category.
func filterByCategory(targets []Target, category nav.Category) []Target {
	if category == nav.CategoryAll {
		return targets
	}

	result := make([]Target, 0, len(targets))
	for _, t := range targets {
		if t.Category == category {
			result = append(result, t)
		}
	}
	return result
}

// NoTargetsInCategoryError indicates no targets exist in a category.
type NoTargetsInCategoryError struct {
	Category nav.Category
}

func (e *NoTargetsInCategoryError) Error() string {
	return fmt.Sprintf("no targets in category: %s", e.Category)
}
