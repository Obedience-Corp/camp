package shortcuts_test

import (
	"bytes"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/shortcuts"
)

// Integration tests for shortcuts expansion workflow

func TestShortcutExpansionEndToEnd(t *testing.T) {
	// Simulate full workflow: user types "p list" and it expands to "project list"
	shortcutMap := config.DefaultNavigationShortcuts()
	expander := shortcuts.NewExpander(shortcutMap)

	tests := []struct {
		name            string
		input           []string
		expectedFirst   string
		shouldExpand    bool
		expectedConcept string
	}{
		{
			name:            "expand p to project",
			input:           []string{"p", "list"},
			expectedFirst:   "project",
			shouldExpand:    true,
			expectedConcept: "project",
		},
		{
			name:            "expand f to festival",
			input:           []string{"f", "status"},
			expectedFirst:   "festival",
			shouldExpand:    true,
			expectedConcept: "festival",
		},
		{
			name:            "expand i to intent",
			input:           []string{"i", "new"},
			expectedFirst:   "intent",
			shouldExpand:    true,
			expectedConcept: "intent",
		},
		{
			name:            "expand cfg to config",
			input:           []string{"cfg", "edit"},
			expectedFirst:   "config",
			shouldExpand:    true,
			expectedConcept: "config",
		},
		{
			name:          "no expansion for full command",
			input:         []string{"project", "list"},
			expectedFirst: "project",
			shouldExpand:  false,
		},
		{
			name:          "no expansion for unknown shortcut",
			input:         []string{"xyz", "list"},
			expectedFirst: "xyz",
			shouldExpand:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expander.Expand(tt.input)

			if result.WasExpanded != tt.shouldExpand {
				t.Errorf("WasExpanded = %v, want %v", result.WasExpanded, tt.shouldExpand)
			}

			if len(result.Expanded) == 0 {
				t.Fatal("Expanded args should not be empty")
			}

			if result.Expanded[0] != tt.expectedFirst {
				t.Errorf("First expanded arg = %q, want %q", result.Expanded[0], tt.expectedFirst)
			}

			if tt.shouldExpand && result.ExpandedTo != tt.expectedConcept {
				t.Errorf("ExpandedTo = %q, want %q", result.ExpandedTo, tt.expectedConcept)
			}
		})
	}
}

func TestNavigationOnlyShortcutError(t *testing.T) {
	shortcutMap := config.DefaultNavigationShortcuts()
	expander := shortcuts.NewExpander(shortcutMap)

	// Test navigation-only shortcuts (d, w, etc.)
	navOnlyShortcuts := []string{"d", "w", "a", "du", "cr", "pi", "de"}

	for _, sc := range navOnlyShortcuts {
		t.Run(sc, func(t *testing.T) {
			_, err := expander.MustExpand([]string{sc, "some", "args"})

			if err == nil {
				t.Errorf("expected error for navigation-only shortcut %q", sc)
				return
			}

			var navErr *shortcuts.NavigationOnlyError
			if !errors.As(err, &navErr) {
				t.Errorf("expected NavigationOnlyError for %q, got %T", sc, err)
				return
			}

			if navErr.Shortcut != sc {
				t.Errorf("NavigationOnlyError.Shortcut = %q, want %q", navErr.Shortcut, sc)
			}
		})
	}
}

func TestFeedbackIntegration(t *testing.T) {
	shortcutMap := config.DefaultNavigationShortcuts()

	var buf bytes.Buffer
	feedback := shortcuts.NewFeedbackWriter(&buf, shortcutMap)
	expander := shortcuts.NewExpander(shortcutMap)

	// Test feedback for successful expansion
	t.Run("expansion feedback", func(t *testing.T) {
		buf.Reset()
		result := expander.Expand([]string{"p", "list"})
		feedback.ShowExpansion(result)

		output := buf.String()
		if !strings.Contains(output, "p") {
			t.Error("expected output to contain shortcut")
		}
		if !strings.Contains(output, "project") {
			t.Error("expected output to contain expanded command")
		}
	})

	// Test feedback for navigation-only shortcut
	t.Run("navigation only feedback", func(t *testing.T) {
		buf.Reset()
		feedback.ShowNavigationOnly("d")

		output := buf.String()
		if !strings.Contains(output, "navigation only") {
			t.Error("expected navigation only message")
		}
		if !strings.Contains(output, "cgo d") {
			t.Error("expected cgo suggestion")
		}
	})

	// Test feedback for unknown shortcut
	t.Run("unknown shortcut feedback", func(t *testing.T) {
		buf.Reset()
		feedback.ShowUnknownShortcut("pp")

		output := buf.String()
		if !strings.Contains(output, "Unknown shortcut") {
			t.Error("expected unknown shortcut message")
		}
		// Should suggest 'p' as similar
		if !strings.Contains(output, "p") {
			t.Error("expected suggestion for similar shortcut")
		}
	})
}

func TestExpandArgsWithDefaultShortcuts(t *testing.T) {
	shortcutMap := config.DefaultNavigationShortcuts()

	tests := []struct {
		input    []string
		expected string
	}{
		{[]string{"p", "list"}, "project"},
		{[]string{"f", "status"}, "festival"},
		{[]string{"cfg", "edit"}, "config"},
		{[]string{"project", "list"}, "project"}, // no change
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.input, "_"), func(t *testing.T) {
			result := shortcuts.ExpandArgs(tt.input, shortcutMap)
			if result[0] != tt.expected {
				t.Errorf("ExpandArgs(%v)[0] = %q, want %q", tt.input, result[0], tt.expected)
			}
		})
	}
}

func TestMergedShortcutsExpansion(t *testing.T) {
	// Test scenario where campaign shortcuts are merged with defaults
	defaults := config.DefaultNavigationShortcuts()

	// Simulate campaign overriding path but inheriting concept
	campaignShortcuts := map[string]config.ShortcutConfig{
		"p": {Path: "my-projects/"}, // Override path, but Concept should be inherited
	}

	// Merge logic (simulating what root.go does)
	merged := make(map[string]config.ShortcutConfig)
	maps.Copy(merged, defaults)
	for k, v := range campaignShortcuts {
		if defaultSc, hasDefault := defaults[k]; hasDefault && v.Concept == "" {
			v.Concept = defaultSc.Concept
		}
		merged[k] = v
	}

	expander := shortcuts.NewExpander(merged)
	result := expander.Expand([]string{"p", "list"})

	if !result.WasExpanded {
		t.Error("expected expansion even with merged shortcuts")
	}
	if result.ExpandedTo != "project" {
		t.Errorf("ExpandedTo = %q, want 'project' (inherited from defaults)", result.ExpandedTo)
	}
}

func TestEmptyArgs(t *testing.T) {
	shortcutMap := config.DefaultNavigationShortcuts()
	expander := shortcuts.NewExpander(shortcutMap)

	result := expander.Expand([]string{})
	if result.WasExpanded {
		t.Error("empty args should not be expanded")
	}

	mustResult, err := expander.MustExpand([]string{})
	if err != nil {
		t.Errorf("MustExpand with empty args should not error: %v", err)
	}
	if mustResult.WasExpanded {
		t.Error("MustExpand with empty args should not expand")
	}
}
