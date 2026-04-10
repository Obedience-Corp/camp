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

	got := BuildCategoryMappings(shortcuts, nil)

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

func TestBuildCategoryMappings_WithPathsMap(t *testing.T) {
	shortcuts := map[string]config.ShortcutConfig{
		"d":  {Path: "docs/"},
		"ai": {Path: "ai_docs/"},
	}
	pathsMap := map[string]string{
		"docs":      "docs/",
		"ai_docs":   "ai_docs/",
		"projects":  "projects/",
		"festivals": "festivals/",
	}

	got := BuildCategoryMappings(shortcuts, pathsMap)

	// Shortcut keys still work
	if got["d"] != CategoryDocs {
		t.Fatalf("shortcut 'd' = %q, want %q", got["d"], CategoryDocs)
	}
	if got["ai"] != CategoryAIDocs {
		t.Fatalf("shortcut 'ai' = %q, want %q", got["ai"], CategoryAIDocs)
	}

	// Concept names from paths map also work
	if got["docs"] != CategoryDocs {
		t.Fatalf("path name 'docs' = %q, want %q", got["docs"], CategoryDocs)
	}
	if got["ai_docs"] != CategoryAIDocs {
		t.Fatalf("path name 'ai_docs' = %q, want %q", got["ai_docs"], CategoryAIDocs)
	}
	if got["projects"] != CategoryProjects {
		t.Fatalf("path name 'projects' = %q, want %q", got["projects"], CategoryProjects)
	}
	if got["festivals"] != CategoryFestivals {
		t.Fatalf("path name 'festivals' = %q, want %q", got["festivals"], CategoryFestivals)
	}
}
