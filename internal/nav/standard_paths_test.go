package nav

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestCategoryForStandardPath(t *testing.T) {
	tests := []struct {
		path string
		want Category
	}{
		{"projects/", CategoryProjects},
		{"projects/worktrees/", CategoryWorktrees},
		{"workflow/design/", CategoryDesign},
		{".campaign/intents/", CategoryIntents},
		{".campaign/intents", CategoryIntents},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := CategoryForStandardPath(tt.path)
			if !ok {
				t.Fatalf("CategoryForStandardPath(%q) did not match", tt.path)
			}
			if got != tt.want {
				t.Fatalf("CategoryForStandardPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCategoryForStandardPath_LegacyIntentPathNotStandard(t *testing.T) {
	for _, path := range []string{"workflow/intents/", "workflow/intents"} {
		t.Run(path, func(t *testing.T) {
			if got, ok := CategoryForStandardPath(path); ok {
				t.Fatalf("CategoryForStandardPath(%q) = %q, want no match", path, got)
			}
		})
	}
}

func TestBuildCategoryMappings(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"p": {
			Path: "projects/",
		},
		"i": {
			Path: ".campaign/intents/",
		},
		"custom": {
			Path: "custom/path/",
		},
	}

	got := BuildCategoryMappings(shortcuts)

	if got["p"] != CategoryProjects {
		t.Fatalf("projects shortcut = %q, want %q", got["p"], CategoryProjects)
	}
	if got["i"] != CategoryIntents {
		t.Fatalf("intents shortcut = %q, want %q", got["i"], CategoryIntents)
	}
	if _, ok := got["custom"]; ok {
		t.Fatal("custom non-standard path should not be mapped to a built-in category")
	}
}

func TestBuildPathAliasMappings(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"d":  {Path: "docs/"},
		"ai": {Path: "ai_docs/"},
		"de": {Path: "workflow/design/"},
		"cx": {Path: "custom/path/"},
	}

	got := BuildPathAliasMappings(shortcuts)

	if got["docs"].Category != CategoryDocs {
		t.Fatalf("docs alias category = %q, want %q", got["docs"].Category, CategoryDocs)
	}
	if got["ai_docs"].Category != CategoryAIDocs {
		t.Fatalf("ai_docs alias category = %q, want %q", got["ai_docs"].Category, CategoryAIDocs)
	}
	if got["design"].Category != CategoryDesign {
		t.Fatalf("design alias category = %q, want %q", got["design"].Category, CategoryDesign)
	}
	if got["path"].RelativePath != "custom/path/" {
		t.Fatalf("path alias relative path = %q, want %q", got["path"].RelativePath, "custom/path/")
	}
}
