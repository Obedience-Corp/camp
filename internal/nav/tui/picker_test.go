package tui

import (
	"errors"
	"testing"

	"github.com/obediencecorp/camp/internal/nav"
)

func TestFormatTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   Target
		expected string
	}{
		{
			name: "with category",
			target: Target{
				Name:     "api-service",
				Category: nav.CategoryProjects,
			},
			expected: "projects/api-service",
		},
		{
			name: "no category",
			target: Target{
				Name:     "api-service",
				Category: nav.CategoryAll,
			},
			expected: "api-service",
		},
		{
			name: "empty category",
			target: Target{
				Name:     "docs",
				Category: "",
			},
			expected: "docs",
		},
		{
			name: "festivals category",
			target: Target{
				Name:     "camp-cli",
				Category: nav.CategoryFestivals,
			},
			expected: "festivals/camp-cli",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTarget(tt.target)
			if result != tt.expected {
				t.Errorf("formatTarget() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFilterByCategory(t *testing.T) {
	targets := []Target{
		{Name: "api-service", Category: nav.CategoryProjects},
		{Name: "web-app", Category: nav.CategoryProjects},
		{Name: "camp-cli", Category: nav.CategoryFestivals},
		{Name: "readme", Category: nav.CategoryDocs},
	}

	tests := []struct {
		name     string
		category nav.Category
		wantLen  int
	}{
		{
			name:     "filter projects",
			category: nav.CategoryProjects,
			wantLen:  2,
		},
		{
			name:     "filter festivals",
			category: nav.CategoryFestivals,
			wantLen:  1,
		},
		{
			name:     "filter docs",
			category: nav.CategoryDocs,
			wantLen:  1,
		},
		{
			name:     "filter all returns everything",
			category: nav.CategoryAll,
			wantLen:  4,
		},
		{
			name:     "filter non-existent returns empty",
			category: nav.CategoryCorpus,
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterByCategory(targets, tt.category)
			if len(result) != tt.wantLen {
				t.Errorf("filterByCategory() returned %d items, want %d", len(result), tt.wantLen)
			}
		})
	}
}

func TestFilterByCategory_PreservesTargets(t *testing.T) {
	targets := []Target{
		{Name: "api-service", Path: "/test/projects/api-service", Category: nav.CategoryProjects},
		{Name: "web-app", Path: "/test/projects/web-app", Category: nav.CategoryProjects},
	}

	result := filterByCategory(targets, nav.CategoryProjects)

	if len(result) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(result))
	}

	if result[0].Name != "api-service" || result[0].Path != "/test/projects/api-service" {
		t.Error("First target not preserved correctly")
	}

	if result[1].Name != "web-app" || result[1].Path != "/test/projects/web-app" {
		t.Error("Second target not preserved correctly")
	}
}

func TestDefaultPickOptions(t *testing.T) {
	opts := DefaultPickOptions()

	if opts.Prompt != "Navigate to: " {
		t.Errorf("Prompt = %q, want %q", opts.Prompt, "Navigate to: ")
	}

	if opts.ShowPreview {
		t.Error("ShowPreview should be false by default")
	}

	if opts.Query != "" {
		t.Errorf("Query should be empty by default, got %q", opts.Query)
	}
}

func TestNoTargetsInCategoryError(t *testing.T) {
	err := &NoTargetsInCategoryError{Category: nav.CategoryProjects}

	msg := err.Error()
	expected := "no targets in category: projects"
	if msg != expected {
		t.Errorf("Error() = %q, want %q", msg, expected)
	}
}

func TestPick_EmptyTargets(t *testing.T) {
	opts := DefaultPickOptions()
	_, err := Pick(nil, opts)

	if !errors.Is(err, ErrNoTargets) {
		t.Errorf("Expected ErrNoTargets, got %v", err)
	}

	_, err = Pick([]Target{}, opts)
	if !errors.Is(err, ErrNoTargets) {
		t.Errorf("Expected ErrNoTargets for empty slice, got %v", err)
	}
}

func TestPickScoped_NoTargetsInCategory(t *testing.T) {
	targets := []Target{
		{Name: "api-service", Category: nav.CategoryProjects},
	}

	opts := DefaultPickOptions()
	_, err := PickScoped(targets, nav.CategoryFestivals, opts)

	if err == nil {
		t.Fatal("Expected error for empty category")
	}

	var catErr *NoTargetsInCategoryError
	if !errors.As(err, &catErr) {
		t.Fatalf("Expected NoTargetsInCategoryError, got %T: %v", err, err)
	}

	if catErr.Category != nav.CategoryFestivals {
		t.Errorf("Category = %q, want %q", catErr.Category, nav.CategoryFestivals)
	}
}

func TestTarget_Fields(t *testing.T) {
	target := Target{
		Name:        "api-service",
		Path:        "/test/projects/api-service",
		Category:    nav.CategoryProjects,
		Description: "Main API service",
	}

	if target.Name != "api-service" {
		t.Errorf("Name = %q, want %q", target.Name, "api-service")
	}
	if target.Path != "/test/projects/api-service" {
		t.Errorf("Path = %q, want %q", target.Path, "/test/projects/api-service")
	}
	if target.Category != nav.CategoryProjects {
		t.Errorf("Category = %q, want %q", target.Category, nav.CategoryProjects)
	}
	if target.Description != "Main API service" {
		t.Errorf("Description = %q, want %q", target.Description, "Main API service")
	}
}

func TestPickResult_Fields(t *testing.T) {
	target := Target{Name: "test", Path: "/test"}
	result := &PickResult{
		Target: target,
		Index:  5,
	}

	if result.Target.Name != "test" {
		t.Error("Target not preserved in result")
	}
	if result.Index != 5 {
		t.Errorf("Index = %d, want 5", result.Index)
	}
}

func BenchmarkFormatTarget(b *testing.B) {
	target := Target{
		Name:     "api-service",
		Category: nav.CategoryProjects,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = formatTarget(target)
	}
}

func BenchmarkFilterByCategory(b *testing.B) {
	targets := make([]Target, 100)
	for i := 0; i < 100; i++ {
		cat := nav.CategoryProjects
		if i%3 == 0 {
			cat = nav.CategoryFestivals
		} else if i%3 == 1 {
			cat = nav.CategoryDocs
		}
		targets[i] = Target{Name: "target", Category: cat}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filterByCategory(targets, nav.CategoryProjects)
	}
}

// Tests for keybindings.go

func TestHelpText(t *testing.T) {
	if HelpText == "" {
		t.Error("HelpText should not be empty")
	}

	// Should contain key navigation hints
	if !contains(HelpText, "navigate") {
		t.Error("HelpText should mention 'navigate'")
	}
	if !contains(HelpText, "select") {
		t.Error("HelpText should mention 'select'")
	}
	if !contains(HelpText, "cancel") {
		t.Error("HelpText should mention 'cancel'")
	}
}

func TestDefaultKeybindings(t *testing.T) {
	if len(DefaultKeybindings) == 0 {
		t.Fatal("DefaultKeybindings should not be empty")
	}

	// Check that essential keybindings are documented
	foundEnter := false
	foundEsc := false
	foundArrow := false

	for _, kb := range DefaultKeybindings {
		if kb.Key == "" {
			t.Error("Keybinding should have a Key")
		}
		if kb.Action == "" {
			t.Error("Keybinding should have an Action")
		}

		if contains(kb.Key, "Enter") {
			foundEnter = true
		}
		if contains(kb.Key, "Esc") {
			foundEsc = true
		}
		if contains(kb.Key, "↑") || contains(kb.Key, "↓") {
			foundArrow = true
		}
	}

	if !foundEnter {
		t.Error("DefaultKeybindings should document Enter key")
	}
	if !foundEsc {
		t.Error("DefaultKeybindings should document Esc key")
	}
	if !foundArrow {
		t.Error("DefaultKeybindings should document arrow keys")
	}
}

func TestErrNotATerminal(t *testing.T) {
	if ErrNotATerminal == nil {
		t.Error("ErrNotATerminal should not be nil")
	}
	if ErrNotATerminal.Error() == "" {
		t.Error("ErrNotATerminal should have a message")
	}
}

func TestIsTerminal(t *testing.T) {
	// In a test environment, stdin is usually not a terminal
	// Just verify the function doesn't panic
	_ = IsTerminal()
}

func TestEnsureTerminal(t *testing.T) {
	// In a test environment, this will likely return error
	// Just verify it returns the expected error type
	err := EnsureTerminal()
	if err != nil {
		if !errors.Is(err, ErrNotATerminal) {
			t.Errorf("Expected ErrNotATerminal, got %v", err)
		}
	}
}

func TestPickOptions_Header(t *testing.T) {
	opts := DefaultPickOptions()
	opts.Header = "Custom header"

	if opts.Header != "Custom header" {
		t.Errorf("Header = %q, want %q", opts.Header, "Custom header")
	}
}

// Helper function for string contains check
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
