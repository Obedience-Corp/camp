package shortcuts

import (
	"errors"
	"reflect"
	"testing"

	"github.com/obediencecorp/camp/internal/config"
)

func TestExpand(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p":   {Path: "projects/", Concept: "project"},
		"d":   {Path: "docs/"}, // no concept - navigation only
		"cfg": {Concept: "config"},
	}
	expander := NewExpander(shortcuts)

	tests := []struct {
		name     string
		args     []string
		expected []string
		expanded bool
	}{
		{
			name:     "expand shortcut with concept",
			args:     []string{"p", "commit", "-m", "msg"},
			expected: []string{"project", "commit", "-m", "msg"},
			expanded: true,
		},
		{
			name:     "no expansion for full command",
			args:     []string{"project", "commit"},
			expected: []string{"project", "commit"},
			expanded: false,
		},
		{
			name:     "no expansion for navigation-only",
			args:     []string{"d", "list"},
			expected: []string{"d", "list"},
			expanded: false,
		},
		{
			name:     "expand concept-only shortcut",
			args:     []string{"cfg", "edit"},
			expected: []string{"config", "edit"},
			expanded: true,
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: []string{},
			expanded: false,
		},
		{
			name:     "single shortcut arg",
			args:     []string{"p"},
			expected: []string{"project"},
			expanded: true,
		},
		{
			name:     "unknown shortcut",
			args:     []string{"xyz", "list"},
			expected: []string{"xyz", "list"},
			expanded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expander.Expand(tt.args)

			if !reflect.DeepEqual(result.Expanded, tt.expected) {
				t.Errorf("Expanded = %v, want %v", result.Expanded, tt.expected)
			}
			if result.WasExpanded != tt.expanded {
				t.Errorf("WasExpanded = %v, want %v", result.WasExpanded, tt.expanded)
			}
		})
	}
}

func TestMustExpand_NavigationOnly(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"d": {Path: "docs/"}, // navigation only
	}
	expander := NewExpander(shortcuts)

	_, err := expander.MustExpand([]string{"d", "list"})
	if err == nil {
		t.Error("expected error for navigation-only shortcut")
	}

	var navErr *NavigationOnlyError
	if !errors.As(err, &navErr) {
		t.Errorf("expected NavigationOnlyError, got %T", err)
	}
}

func TestMustExpand_Success(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p": {Path: "projects/", Concept: "project"},
	}
	expander := NewExpander(shortcuts)

	result, err := expander.MustExpand([]string{"p", "list"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !result.WasExpanded {
		t.Error("expected expansion to occur")
	}
	if result.ExpandedTo != "project" {
		t.Errorf("ExpandedTo = %s, want project", result.ExpandedTo)
	}
}

func TestMustExpand_NotAShortcut(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p": {Path: "projects/", Concept: "project"},
	}
	expander := NewExpander(shortcuts)

	result, err := expander.MustExpand([]string{"project", "list"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.WasExpanded {
		t.Error("expected no expansion")
	}
}

func TestExpandArgs(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p": {Path: "projects/", Concept: "project"},
	}

	result := ExpandArgs([]string{"p", "list"}, shortcuts)
	expected := []string{"project", "list"}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("ExpandArgs = %v, want %v", result, expected)
	}
}

func TestNavigationOnlyError_Error(t *testing.T) {
	err := &NavigationOnlyError{
		Shortcut: "d",
		Path:     "docs/",
	}

	msg := err.Error()
	if msg == "" {
		t.Error("error message should not be empty")
	}
	if !contains(msg, "d") || !contains(msg, "docs/") {
		t.Errorf("error message should contain shortcut and path, got: %s", msg)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
