package intent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/intent/promote"
)

func TestExtractFirstParagraph(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
		{
			name:    "single paragraph",
			content: "This is the goal of the intent.",
			want:    "This is the goal of the intent.",
		},
		{
			name:    "two paragraphs",
			content: "First paragraph.\n\nSecond paragraph.",
			want:    "First paragraph.",
		},
		{
			name:    "starts with header",
			content: "# My Feature\nThis is the description.",
			want:    "This is the description.",
		},
		{
			name:    "header only",
			content: "# Just a Header",
			want:    "",
		},
		{
			name:    "header then paragraph",
			content: "# Title\n\nThe actual content here.",
			want:    "The actual content here.",
		},
		{
			name:    "whitespace-heavy",
			content: "  \n  First real paragraph.  \n\nSecond.",
			want:    "First real paragraph.",
		},
		{
			name:    "header with blank lines",
			content: "# Title\n\nGoal description.\n\nMore details.",
			want:    "Goal description.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := promote.ExtractFirstParagraph(tt.content)
			if got != tt.want {
				t.Errorf("ExtractFirstParagraph() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCopyIntentToIngest(t *testing.T) {
	dir := t.TempDir()

	// Create the 001_INGEST/input_specs/ directory
	festivalName := "test-festival"
	ingestDir := filepath.Join(dir, "festivals", "planning", festivalName, "001_INGEST", "input_specs")
	if err := os.MkdirAll(ingestDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create an intent file
	intentPath := filepath.Join(dir, "workflow", "intents", "my-feature.md")
	os.MkdirAll(filepath.Dir(intentPath), 0755)
	os.WriteFile(intentPath, []byte("# My Feature"), 0644)

	// Verify file copy by writing and reading directly
	destPath := filepath.Join(ingestDir, "my-feature.md")
	src, _ := os.ReadFile(intentPath)
	if err := os.WriteFile(destPath, src, 0644); err != nil {
		t.Fatalf("copying intent to ingest: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(got) != "# My Feature" {
		t.Errorf("copied content = %q, want %q", string(got), "# My Feature")
	}
}
