package commit

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeFiles(t *testing.T) {
	defaultRoot := filepath.Join(string(filepath.Separator), "tmp", "campaign")

	tests := []struct {
		name     string
		root     string
		input    []string
		expected []string
	}{
		{
			name: "normalizes absolute and relative paths",
			root: defaultRoot,
			input: []string{
				filepath.Join(defaultRoot, "workflow", "intents", "inbox", "a.md"),
				filepath.Join("workflow", "intents", "active", "b.md"),
			},
			expected: []string{
				filepath.Join("workflow", "intents", "inbox", "a.md"),
				filepath.Join("workflow", "intents", "active", "b.md"),
			},
		},
		{
			name: "deduplicates and drops dot entries",
			root: defaultRoot,
			input: []string{
				filepath.Join(defaultRoot, "workflow", "intents", "inbox", "a.md"),
				filepath.Join("workflow", "intents", "inbox", "a.md"),
				".",
				"",
			},
			expected: []string{
				filepath.Join("workflow", "intents", "inbox", "a.md"),
			},
		},
		{
			name: "drops paths outside root",
			root: defaultRoot,
			input: []string{
				filepath.Join(defaultRoot, "workflow", "intents", "inbox", "a.md"),
				filepath.Join(defaultRoot, "..", "outside.txt"),
				filepath.Join("..", "outside2.txt"),
			},
			expected: []string{
				filepath.Join("workflow", "intents", "inbox", "a.md"),
			},
		},
		{
			name: "keeps absolute paths when root is unknown",
			root: "",
			input: []string{
				filepath.Join(string(filepath.Separator), "tmp", "a.txt"),
			},
			expected: []string{
				filepath.Join(string(filepath.Separator), "tmp", "a.txt"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFiles(tt.root, tt.input...)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("NormalizeFiles() = %#v, want %#v", got, tt.expected)
			}
		})
	}
}
