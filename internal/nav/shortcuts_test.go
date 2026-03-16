package nav

import (
	"testing"
)

func TestParseShortcut_EmptyArgs(t *testing.T) {
	result := ParseShortcut(nil, nil)

	if result.Category != CategoryAll {
		t.Errorf("Category = %q, want %q", result.Category, CategoryAll)
	}
	if result.Query != "" {
		t.Errorf("Query = %q, want empty", result.Query)
	}
	if result.IsShortcut {
		t.Error("IsShortcut should be false")
	}
}

func TestParseShortcut_SingleLetterShortcuts(t *testing.T) {
	tests := []struct {
		args     []string
		category Category
		query    string
	}{
		{[]string{"p"}, CategoryProjects, ""},
		{[]string{"pw"}, CategoryWorktrees, ""},
		{[]string{"f"}, CategoryFestivals, ""},
		{[]string{"ai"}, CategoryAIDocs, ""},
		{[]string{"d"}, CategoryDocs, ""},
		{[]string{"du"}, CategoryDungeon, ""},
		{[]string{"w"}, CategoryWorkflow, ""},
		{[]string{"cr"}, CategoryCodeReviews, ""},
		{[]string{"de"}, CategoryDesign, ""},
		{[]string{"i"}, CategoryIntents, ""},
	}

	for _, tt := range tests {
		t.Run(tt.args[0], func(t *testing.T) {
			// Must pass shortcuts explicitly - no defaults used
			result := ParseShortcut(tt.args, DefaultShortcuts)

			if result.Category != tt.category {
				t.Errorf("Category = %q, want %q", result.Category, tt.category)
			}
			if result.Query != tt.query {
				t.Errorf("Query = %q, want %q", result.Query, tt.query)
			}
			if !result.IsShortcut {
				t.Error("IsShortcut should be true")
			}
		})
	}
}

func TestParseShortcut_TwoLetterShortcut(t *testing.T) {
	// Must pass shortcuts explicitly - no defaults used
	result := ParseShortcut([]string{"pi"}, DefaultShortcuts)

	if result.Category != CategoryPipelines {
		t.Errorf("Category = %q, want %q", result.Category, CategoryPipelines)
	}
	if result.Query != "" {
		t.Errorf("Query = %q, want empty", result.Query)
	}
	if !result.IsShortcut {
		t.Error("IsShortcut should be true")
	}
}

func TestParseShortcut_ShortcutWithQuery(t *testing.T) {
	tests := []struct {
		args     []string
		category Category
		query    string
	}{
		{[]string{"p", "api"}, CategoryProjects, "api"},
		{[]string{"p", "api", "service"}, CategoryProjects, "api service"},
		{[]string{"f", "camp-cli"}, CategoryFestivals, "camp-cli"},
		{[]string{"pi", "data", "pipeline"}, CategoryPipelines, "data pipeline"},
	}

	for _, tt := range tests {
		name := tt.args[0] + " " + tt.args[1]
		t.Run(name, func(t *testing.T) {
			// Must pass shortcuts explicitly - no defaults used
			result := ParseShortcut(tt.args, DefaultShortcuts)

			if result.Category != tt.category {
				t.Errorf("Category = %q, want %q", result.Category, tt.category)
			}
			if result.Query != tt.query {
				t.Errorf("Query = %q, want %q", result.Query, tt.query)
			}
			if !result.IsShortcut {
				t.Error("IsShortcut should be true")
			}
		})
	}
}

func TestParseShortcut_NoShortcutMatch(t *testing.T) {
	tests := []struct {
		args  []string
		query string
	}{
		{[]string{"api-service"}, "api-service"},
		{[]string{"unknown"}, "unknown"},
		{[]string{"x"}, "x"},
		{[]string{"search", "term"}, "search term"},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := ParseShortcut(tt.args, nil)

			if result.Category != CategoryAll {
				t.Errorf("Category = %q, want %q", result.Category, CategoryAll)
			}
			if result.Query != tt.query {
				t.Errorf("Query = %q, want %q", result.Query, tt.query)
			}
			if result.IsShortcut {
				t.Error("IsShortcut should be false")
			}
		})
	}
}

func TestParseShortcut_CaseInsensitive(t *testing.T) {
	// Shortcuts should be case-insensitive
	tests := []struct {
		input    string
		category Category
	}{
		{"p", CategoryProjects},
		{"P", CategoryProjects},
		{"PI", CategoryPipelines},
		{"Pi", CategoryPipelines},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Must pass shortcuts explicitly - no defaults used
			result := ParseShortcut([]string{tt.input}, DefaultShortcuts)

			if result.Category != tt.category {
				t.Errorf("Category = %q, want %q", result.Category, tt.category)
			}
			if !result.IsShortcut {
				t.Error("IsShortcut should be true")
			}
		})
	}
}

func TestParseShortcut_CustomMappings(t *testing.T) {
	custom := map[string]Category{
		"x":  CategoryProjects,    // New shortcut
		"p":  CategoryPipelines,   // Override default
		"ab": CategoryCodeReviews, // New two-letter
	}

	tests := []struct {
		args     []string
		category Category
	}{
		{[]string{"x"}, CategoryProjects},     // Custom shortcut works
		{[]string{"p"}, CategoryPipelines},    // Override works
		{[]string{"ab"}, CategoryCodeReviews}, // Custom two-letter works
	}

	for _, tt := range tests {
		t.Run(tt.args[0], func(t *testing.T) {
			result := ParseShortcut(tt.args, custom)

			if result.Category != tt.category {
				t.Errorf("Category = %q, want %q", result.Category, tt.category)
			}
		})
	}
}

func TestParseShortcut_NoDefaultFallback(t *testing.T) {
	// Test that shortcuts not in custom mappings don't fall back to defaults
	custom := map[string]Category{
		"x": CategoryProjects, // Only define "x"
	}

	// "p" is in DefaultShortcuts but not in custom, should NOT match
	result := ParseShortcut([]string{"p"}, custom)

	if result.Category != CategoryAll {
		t.Errorf("Category = %q, want %q (no fallback to defaults)", result.Category, CategoryAll)
	}
	if result.IsShortcut {
		t.Error("IsShortcut should be false - no fallback to defaults")
	}
	if result.Query != "p" {
		t.Errorf("Query = %q, want %q", result.Query, "p")
	}
}

func TestCategoryDir(t *testing.T) {
	tests := []struct {
		category Category
		dir      string
	}{
		{CategoryProjects, "projects"},
		{CategoryWorktrees, "projects/worktrees"},
		{CategoryFestivals, "festivals"},
		{CategoryAIDocs, "ai_docs"},
		{CategoryDocs, "docs"},
		{CategoryDungeon, "dungeon"},
		{CategoryWorkflow, "workflow"},
		{CategoryCodeReviews, "workflow/code_reviews"},
		{CategoryPipelines, "workflow/pipelines"},
		{CategoryDesign, "workflow/design"},
		{CategoryIntents, "workflow/intents"},
		{CategoryAll, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			dir := tt.category.Dir()
			if dir != tt.dir {
				t.Errorf("Dir() = %q, want %q", dir, tt.dir)
			}
		})
	}
}

func TestCategoryString(t *testing.T) {
	if s := CategoryProjects.String(); s != "projects" {
		t.Errorf("CategoryProjects.String() = %q, want %q", s, "projects")
	}
	if s := CategoryAll.String(); s != "all" {
		t.Errorf("CategoryAll.String() = %q, want %q", s, "all")
	}
}

func TestValidCategories(t *testing.T) {
	cats := ValidCategories()
	if len(cats) != 11 {
		t.Errorf("len(ValidCategories()) = %d, want 11", len(cats))
	}

	// Verify all expected categories are present
	expected := map[Category]bool{
		CategoryProjects:    false,
		CategoryWorktrees:   false,
		CategoryFestivals:   false,
		CategoryAIDocs:      false,
		CategoryDocs:        false,
		CategoryDungeon:     false,
		CategoryWorkflow:    false,
		CategoryCodeReviews: false,
		CategoryPipelines:   false,
		CategoryDesign:      false,
		CategoryIntents:     false,
	}

	for _, c := range cats {
		if _, ok := expected[c]; !ok {
			t.Errorf("unexpected category: %q", c)
		}
		expected[c] = true
	}

	for c, found := range expected {
		if !found {
			t.Errorf("missing category: %q", c)
		}
	}
}

func TestShortcutForCategory(t *testing.T) {
	tests := []struct {
		category Category
		shortcut string
	}{
		{CategoryProjects, "p"},
		{CategoryWorktrees, "pw"},
		{CategoryDungeon, "du"},
		{CategoryWorkflow, "w"},
		{CategoryPipelines, "pi"},
		{CategoryDesign, "de"},
		{CategoryIntents, "i"},
		{CategoryAll, ""}, // No shortcut for all
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			s := ShortcutForCategory(tt.category)
			if s != tt.shortcut {
				t.Errorf("ShortcutForCategory(%q) = %q, want %q", tt.category, s, tt.shortcut)
			}
		})
	}
}

func TestDefaultShortcutsComplete(t *testing.T) {
	// Verify all shortcuts are defined
	expectedCount := 11 // p, pw, f, a, d, du, w, cr, pi, de, i
	if len(DefaultShortcuts) != expectedCount {
		t.Errorf("len(DefaultShortcuts) = %d, want %d", len(DefaultShortcuts), expectedCount)
	}

	// Verify all categories have a shortcut
	for _, cat := range ValidCategories() {
		found := false
		for _, c := range DefaultShortcuts {
			if c == cat {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("category %q has no shortcut defined", cat)
		}
	}
}
