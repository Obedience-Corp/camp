package complete

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/nav"
)

func TestCategories(t *testing.T) {
	cats := Categories()

	// Check we have all expected categories
	expected := map[string]string{
		"p":  "projects/",
		"pw": "projects/worktrees/",
		"f":  "festivals/",
		"a":  "ai_docs/",
		"d":  "docs/",
		"du": "dungeon/",
		"w":  "workflow/",
		"cr": "workflow/code_reviews/",
		"pi": "workflow/pipelines/",
		"de": "workflow/design/",
		"i":  "workflow/intents/",
	}

	if len(cats) != len(expected) {
		t.Errorf("Got %d categories, want %d", len(cats), len(expected))
	}

	for _, c := range cats {
		desc, ok := expected[c.Value]
		if !ok {
			t.Errorf("Unexpected category shortcut: %s", c.Value)
			continue
		}
		if c.Description != desc {
			t.Errorf("Category %s description = %q, want %q", c.Value, c.Description, desc)
		}
	}
}

func TestCategories_HasCorrectCategories(t *testing.T) {
	cats := Categories()

	categoryMap := make(map[string]nav.Category)
	for _, c := range cats {
		categoryMap[c.Value] = c.Category
	}

	tests := []struct {
		shortcut string
		want     nav.Category
	}{
		{"p", nav.CategoryProjects},
		{"pw", nav.CategoryWorktrees},
		{"f", nav.CategoryFestivals},
		{"a", nav.CategoryAIDocs},
		{"d", nav.CategoryDocs},
		{"du", nav.CategoryDungeon},
		{"w", nav.CategoryWorkflow},
		{"cr", nav.CategoryCodeReviews},
		{"pi", nav.CategoryPipelines},
		{"de", nav.CategoryDesign},
		{"i", nav.CategoryIntents},
	}

	for _, tt := range tests {
		t.Run(tt.shortcut, func(t *testing.T) {
			got := categoryMap[tt.shortcut]
			if got != tt.want {
				t.Errorf("Category for %s = %v, want %v", tt.shortcut, got, tt.want)
			}
		})
	}
}

func TestCampaigns_NoRegistry(t *testing.T) {
	ctx := context.Background()

	// Should return nil or empty if no registry exists
	campaigns := Campaigns(ctx)
	_ = campaigns // Can be nil or empty, both acceptable
}

func TestCampaigns_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should return nil on cancelled context
	campaigns := Campaigns(ctx)
	if campaigns != nil && len(campaigns) > 0 {
		t.Error("expected nil or empty for cancelled context")
	}
}

func TestGenerateWithDescriptions_NoArgs(t *testing.T) {
	ctx := context.Background()

	candidates := GenerateWithDescriptions(ctx, nil)

	// Should have at least the category shortcuts
	if len(candidates) < 8 {
		t.Errorf("Got %d candidates, want at least 8 category shortcuts", len(candidates))
	}

	// First candidates should be category shortcuts
	shortcuts := map[string]bool{"p": true, "pw": true, "f": true, "a": true, "d": true, "du": true, "w": true, "cr": true, "pi": true, "de": true, "i": true}
	foundShortcuts := 0
	for _, c := range candidates {
		if shortcuts[c.Value] {
			foundShortcuts++
		}
	}
	if foundShortcuts != 11 {
		t.Errorf("Found %d shortcuts, want 11", foundShortcuts)
	}
}

func TestGenerateWithDescriptions_WithArgs(t *testing.T) {
	// Create test campaign
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	projPath := filepath.Join(root, "projects", "test-project")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()
	candidates := GenerateWithDescriptions(ctx, []string{"p"})

	if len(candidates) == 0 {
		t.Error("expected some candidates")
	}
}

func TestFormatForShell_Fish(t *testing.T) {
	candidates := []CategoryCandidate{
		{Value: "p", Description: "projects/"},
		{Value: "test", Description: ""},
	}

	output := FormatForShell(candidates, "fish")

	if len(output) != 2 {
		t.Fatalf("Got %d outputs, want 2", len(output))
	}

	// Fish should have tab-separated description
	if output[0] != "p\tprojects/" {
		t.Errorf("Fish output[0] = %q, want %q", output[0], "p\tprojects/")
	}

	// No description should just be value
	if output[1] != "test" {
		t.Errorf("Fish output[1] = %q, want %q", output[1], "test")
	}
}

func TestFormatForShell_Bash(t *testing.T) {
	candidates := []CategoryCandidate{
		{Value: "p", Description: "projects/"},
		{Value: "test", Description: "some desc"},
	}

	output := FormatForShell(candidates, "bash")

	if len(output) != 2 {
		t.Fatalf("Got %d outputs, want 2", len(output))
	}

	// Bash should just have values
	if output[0] != "p" {
		t.Errorf("Bash output[0] = %q, want %q", output[0], "p")
	}
	if output[1] != "test" {
		t.Errorf("Bash output[1] = %q, want %q", output[1], "test")
	}
}

func TestFormatForShell_Zsh(t *testing.T) {
	candidates := []CategoryCandidate{
		{Value: "p", Description: "projects/"},
	}

	output := FormatForShell(candidates, "zsh")

	if len(output) != 1 {
		t.Fatalf("Got %d outputs, want 1", len(output))
	}

	// Zsh (default) should just have values
	if output[0] != "p" {
		t.Errorf("Zsh output[0] = %q, want %q", output[0], "p")
	}
}

func TestCategoryByShortcut(t *testing.T) {
	tests := []struct {
		shortcut string
		want     nav.Category
	}{
		{"p", nav.CategoryProjects},
		{"pw", nav.CategoryWorktrees},
		{"f", nav.CategoryFestivals},
		{"a", nav.CategoryAIDocs},
		{"d", nav.CategoryDocs},
		{"du", nav.CategoryDungeon},
		{"w", nav.CategoryWorkflow},
		{"cr", nav.CategoryCodeReviews},
		{"pi", nav.CategoryPipelines},
		{"de", nav.CategoryDesign},
		{"i", nav.CategoryIntents},
		{"invalid", nav.CategoryAll},
		{"", nav.CategoryAll},
	}

	for _, tt := range tests {
		t.Run(tt.shortcut, func(t *testing.T) {
			got := CategoryByShortcut(tt.shortcut)
			if got != tt.want {
				t.Errorf("CategoryByShortcut(%q) = %v, want %v", tt.shortcut, got, tt.want)
			}
		})
	}
}

func TestDescriptionForCategory(t *testing.T) {
	tests := []struct {
		shortcut string
		want     string
	}{
		{"p", "projects/"},
		{"w", "workflow/"},
		{"f", "festivals/"},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.shortcut, func(t *testing.T) {
			got := DescriptionForCategory(tt.shortcut)
			if got != tt.want {
				t.Errorf("DescriptionForCategory(%q) = %q, want %q", tt.shortcut, got, tt.want)
			}
		})
	}
}

// Benchmarks

func BenchmarkCategories(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Categories()
	}
}

func BenchmarkFormatForShell_Fish(b *testing.B) {
	candidates := Categories()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = FormatForShell(candidates, "fish")
	}
}

func BenchmarkGenerateWithDescriptions(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = GenerateWithDescriptions(ctx, nil)
	}
}
