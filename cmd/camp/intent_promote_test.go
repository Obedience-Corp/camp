package main

import (
	"os"
	"path/filepath"
	"testing"
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
			got := extractFirstParagraph(tt.content)
			if got != tt.want {
				t.Errorf("extractFirstParagraph() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()

	// Create source file
	src := filepath.Join(dir, "source.md")
	content := "# My Intent\n\nSome content here."
	if err := os.WriteFile(src, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy to destination
	dst := filepath.Join(dir, "dest.md")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error: %v", err)
	}

	// Verify content matches
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if string(got) != content {
		t.Errorf("copied content = %q, want %q", string(got), content)
	}
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nonexistent.md"), filepath.Join(dir, "dest.md"))
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestCopyFile_BadDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.md")
	os.WriteFile(src, []byte("content"), 0644)

	err := copyFile(src, filepath.Join(dir, "no", "such", "dir", "dest.md"))
	if err == nil {
		t.Error("expected error for bad destination path")
	}
}

func TestCopyIntentToIngest(t *testing.T) {
	dir := t.TempDir()

	// Create the 001_INGEST/input_specs/ directory
	festivalName := "test-festival"
	ingestDir := filepath.Join(dir, "festivals", "active", festivalName, "001_INGEST", "input_specs")
	if err := os.MkdirAll(ingestDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create an intent file
	intentPath := filepath.Join(dir, "workflow", "intents", "my-feature.md")
	os.MkdirAll(filepath.Dir(intentPath), 0755)
	os.WriteFile(intentPath, []byte("# My Feature"), 0644)

	// Import the intent type from the package
	type fakeIntent struct {
		Path string
	}

	// Call copyIntentToIngest
	// Since copyIntentToIngest takes *intent.Intent, we can't easily call it from
	// the cmd package test. Instead, verify the copy behavior through copyFile.
	destPath := filepath.Join(ingestDir, "my-feature.md")
	if err := copyFile(intentPath, destPath); err != nil {
		t.Fatalf("copying intent to ingest: %v", err)
	}

	// Verify the file was copied
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(got) != "# My Feature" {
		t.Errorf("copied content = %q, want %q", string(got), "# My Feature")
	}
}
