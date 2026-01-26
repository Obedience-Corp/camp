package shortcuts

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/obediencecorp/camp/internal/config"
)

func TestFeedbackWriter_ShowExpansion(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p": {Path: "projects/", Concept: "project"},
	}

	var buf bytes.Buffer
	fw := NewFeedbackWriter(&buf, shortcuts)

	result := ExpansionResult{
		Original:    []string{"p", "list"},
		Expanded:    []string{"project", "list"},
		WasExpanded: true,
		Shortcut:    "p",
		ExpandedTo:  "project",
	}

	fw.ShowExpansion(result)

	output := buf.String()
	if !strings.Contains(output, "expanded") {
		t.Error("expected output to contain 'expanded'")
	}
	if !strings.Contains(output, "p") {
		t.Error("expected output to contain shortcut 'p'")
	}
	if !strings.Contains(output, "project") {
		t.Error("expected output to contain 'project'")
	}
}

func TestFeedbackWriter_ShowExpansion_NoExpansion(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{}

	var buf bytes.Buffer
	fw := NewFeedbackWriter(&buf, shortcuts)

	result := ExpansionResult{
		Original:    []string{"project", "list"},
		Expanded:    []string{"project", "list"},
		WasExpanded: false,
	}

	fw.ShowExpansion(result)

	if buf.Len() != 0 {
		t.Errorf("expected no output when not expanded, got: %s", buf.String())
	}
}

func TestFeedbackWriter_ShowNavigationOnly(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"d": {Path: "docs/", Description: "Docs directory"},
	}

	var buf bytes.Buffer
	fw := NewFeedbackWriter(&buf, shortcuts)

	fw.ShowNavigationOnly("d")

	output := buf.String()
	if !strings.Contains(output, "navigation only") {
		t.Error("expected output to contain 'navigation only'")
	}
	if !strings.Contains(output, "docs/") {
		t.Error("expected output to contain path 'docs/'")
	}
	if !strings.Contains(output, "cgo d") {
		t.Error("expected output to contain 'cgo d' suggestion")
	}
}

func TestFeedbackWriter_ShowUnknownShortcut(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p":   {Path: "projects/", Concept: "project", Description: "Projects"},
		"pr":  {Path: "projects/", Description: "Projects alternate"},
		"cfg": {Concept: "config"},
	}

	var buf bytes.Buffer
	fw := NewFeedbackWriter(&buf, shortcuts)

	fw.ShowUnknownShortcut("pp") // Typo for 'p'

	output := buf.String()
	if !strings.Contains(output, "Unknown shortcut") {
		t.Error("expected output to contain 'Unknown shortcut'")
	}
	if !strings.Contains(output, "Did you mean") {
		t.Error("expected output to contain suggestions")
	}
	if !strings.Contains(output, "camp shortcuts") {
		t.Error("expected output to suggest 'camp shortcuts'")
	}
}

func TestFeedbackWriter_ShowAvailableShortcuts(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p":   {Path: "projects/", Concept: "project", Description: "Projects"},
		"d":   {Path: "docs/", Description: "Docs"},
		"cfg": {Concept: "config", Description: "Config commands"},
	}

	var buf bytes.Buffer
	fw := NewFeedbackWriter(&buf, shortcuts)

	fw.ShowAvailableShortcuts()

	output := buf.String()
	// Should show command shortcuts
	if !strings.Contains(output, "Command shortcuts") {
		t.Error("expected output to contain 'Command shortcuts'")
	}
	// Should show navigation shortcuts
	if !strings.Contains(output, "Navigation shortcuts") {
		t.Error("expected output to contain 'Navigation shortcuts'")
	}
}

func TestFeedbackWriter_ShowAvailableShortcuts_Empty(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{}

	var buf bytes.Buffer
	fw := NewFeedbackWriter(&buf, shortcuts)

	fw.ShowAvailableShortcuts()

	output := buf.String()
	if !strings.Contains(output, "No shortcuts") {
		t.Error("expected output to indicate no shortcuts")
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"abc", "abc", 0},
		{"abc", "ab", 1},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"p", "pp", 1},
		{"project", "porject", 2},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			result := levenshtein(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestFindSimilar(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p":   {Concept: "project"},
		"pr":  {Path: "projects/"},
		"cfg": {Concept: "config"},
		"f":   {Concept: "festival"},
	}

	fw := NewFeedbackWriter(nil, shortcuts)

	tests := []struct {
		target   string
		expected []string
	}{
		{"pp", []string{"p"}},       // typo
		{"cfgg", []string{"cfg"}},   // typo
		{"xxxxxxxxx", []string{}},   // no match
		{"pr", []string{"pr", "p"}}, // exact match first
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			result := fw.findSimilar(tt.target, 3)

			// Check that expected items are in result
			for _, exp := range tt.expected {
				if !slices.Contains(result, exp) {
					t.Errorf("findSimilar(%q) = %v, expected to contain %q", tt.target, result, exp)
				}
			}
		})
	}
}
