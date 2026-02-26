// Package shortcuts provides shortcut expansion and handling for the camp CLI.
package shortcuts

import (
	"fmt"

	"github.com/Obedience-Corp/camp/internal/config"
)

// ExpansionResult contains the result of shortcut expansion.
type ExpansionResult struct {
	Original    []string // Original arguments
	Expanded    []string // Expanded arguments
	WasExpanded bool     // Whether expansion occurred
	Shortcut    string   // The shortcut that was expanded
	ExpandedTo  string   // What it expanded to
}

// Expander handles shortcut-to-command expansion.
type Expander struct {
	shortcuts map[string]config.ShortcutConfig
}

// NewExpander creates an expander with the given shortcuts.
func NewExpander(shortcuts map[string]config.ShortcutConfig) *Expander {
	return &Expander{shortcuts: shortcuts}
}

// Expand checks if the first argument is a shortcut and expands it.
func (e *Expander) Expand(args []string) ExpansionResult {
	result := ExpansionResult{
		Original: args,
		Expanded: args,
	}

	// Need at least one argument to expand
	if len(args) == 0 {
		return result
	}

	firstArg := args[0]

	// Check if it's a known shortcut
	sc, ok := e.shortcuts[firstArg]
	if !ok {
		return result
	}

	// Check if shortcut has a concept mapping
	if sc.Concept == "" {
		return result // Navigation-only shortcut
	}

	// Expand the shortcut
	expanded := make([]string, len(args))
	expanded[0] = sc.Concept
	copy(expanded[1:], args[1:])

	result.Expanded = expanded
	result.WasExpanded = true
	result.Shortcut = firstArg
	result.ExpandedTo = sc.Concept

	return result
}

// ExpandArgs is a convenience function for simple expansion.
func ExpandArgs(args []string, shortcuts map[string]config.ShortcutConfig) []string {
	expander := NewExpander(shortcuts)
	return expander.Expand(args).Expanded
}

// MustExpand expands and returns an error if the shortcut is navigation-only.
func (e *Expander) MustExpand(args []string) (ExpansionResult, error) {
	if len(args) == 0 {
		return ExpansionResult{Original: args, Expanded: args}, nil
	}

	firstArg := args[0]
	sc, ok := e.shortcuts[firstArg]

	// Not a shortcut - return unchanged
	if !ok {
		return ExpansionResult{Original: args, Expanded: args}, nil
	}

	// Shortcut found but no concept
	if sc.Concept == "" {
		return ExpansionResult{}, &NavigationOnlyError{
			Shortcut: firstArg,
			Path:     sc.Path,
		}
	}

	// Normal expansion
	return e.Expand(args), nil
}

// NavigationOnlyError is returned when trying to use a navigation-only shortcut.
type NavigationOnlyError struct {
	Shortcut string
	Path     string
}

func (e *NavigationOnlyError) Error() string {
	return fmt.Sprintf(
		"shortcut '%s' is for navigation only (path: %s)\n"+
			"It has no associated command. Use 'cgo %s' to navigate.",
		e.Shortcut, e.Path, e.Shortcut)
}
