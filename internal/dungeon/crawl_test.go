package dungeon

import (
	"strings"
	"testing"
	"time"
)

func TestBuildInfoString(t *testing.T) {
	tests := []struct {
		name     string
		item     DungeonItem
		stats    *ItemStats
		contains []string
	}{
		{
			name: "file without stats",
			item: DungeonItem{
				Name:    "test.txt",
				Type:    ItemTypeFile,
				ModTime: time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC),
			},
			stats:    nil,
			contains: []string{"Type: file", "Modified: 2026-01-22"},
		},
		{
			name: "directory without stats",
			item: DungeonItem{
				Name:    "test-dir/",
				Type:    ItemTypeDirectory,
				ModTime: time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC),
			},
			stats:    nil,
			contains: []string{"Type: directory", "Modified: 2026-01-22"},
		},
		{
			name: "file with scc stats",
			item: DungeonItem{
				Name:    "project/",
				Type:    ItemTypeDirectory,
				ModTime: time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC),
			},
			stats: &ItemStats{
				Files:  10,
				Code:   500,
				Source: "scc",
			},
			contains: []string{"Files: 10", "Code: 500 lines"},
		},
		{
			name: "file with fest stats (tokens)",
			item: DungeonItem{
				Name:    "doc.md",
				Type:    ItemTypeFile,
				ModTime: time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC),
			},
			stats: &ItemStats{
				Files:  1,
				Lines:  100,
				Tokens: 1500,
				Source: "fest",
			},
			contains: []string{"Files: 1", "Lines: 100", "Tokens: 1500"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildInfoString(tt.item, tt.stats)

			for _, substr := range tt.contains {
				if !containsSubstring(result, substr) {
					t.Errorf("buildInfoString() = %q, should contain %q", result, substr)
				}
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || containsSubstring(s[1:], substr)))
}

func TestBuildInfoString_CodeVsLines(t *testing.T) {
	// When Code is available, it should be preferred over Lines
	item := DungeonItem{
		Name:    "test",
		Type:    ItemTypeFile,
		ModTime: time.Now(),
	}

	statsWithCode := &ItemStats{
		Lines: 100,
		Code:  80,
	}

	result := buildInfoString(item, statsWithCode)
	if !containsSubstring(result, "Code: 80 lines") {
		t.Errorf("should show Code when available, got: %s", result)
	}

	// When only Lines is available
	statsWithLines := &ItemStats{
		Lines: 100,
	}

	result = buildInfoString(item, statsWithLines)
	if !containsSubstring(result, "Lines: 100") {
		t.Errorf("should show Lines when Code not available, got: %s", result)
	}
}

func TestMoveErrorHint_InvalidItemPath(t *testing.T) {
	hint := moveErrorHint(ErrInvalidItemPath)
	if hint == "" {
		t.Fatal("moveErrorHint should return guidance for ErrInvalidItemPath")
	}
	if !strings.Contains(hint, "direct child") {
		t.Fatalf("hint should mention direct child requirement, got: %q", hint)
	}
}
