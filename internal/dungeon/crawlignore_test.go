package dungeon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCrawlIgnore(t *testing.T) {
	tests := []struct {
		name    string
		content string
		input   string
		isDir   bool
		want    bool
	}{
		{
			name:    "simple glob matches",
			content: "*.log",
			input:   "debug.log",
			want:    true,
		},
		{
			name:    "simple glob no match",
			content: "*.log",
			input:   "readme.md",
			want:    false,
		},
		{
			name:    "exact name match",
			content: "temp",
			input:   "temp",
			want:    true,
		},
		{
			name:    "exact name no match",
			content: "temp",
			input:   "other",
			want:    false,
		},
		{
			name:    "negation excludes then includes",
			content: "*.log\n!important.log",
			input:   "important.log",
			want:    false,
		},
		{
			name:    "negation other still excluded",
			content: "*.log\n!important.log",
			input:   "debug.log",
			want:    true,
		},
		{
			name:    "comment lines ignored",
			content: "# this is a comment\n*.tmp",
			input:   "old.tmp",
			want:    true,
		},
		{
			name:    "empty lines ignored",
			content: "\n\n*.bak\n\n",
			input:   "file.bak",
			want:    true,
		},
		{
			name:    "multiple patterns",
			content: "*.tmp\n*.bak\n*.log",
			input:   "old.bak",
			want:    true,
		},
		{
			name:    "prefix glob",
			content: "test-*",
			input:   "test-output",
			want:    true,
		},
		{
			name:    "prefix glob no match",
			content: "test-*",
			input:   "production-output",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, CrawlIgnoreFile)
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write test file: %v", err)
			}

			m, err := LoadCrawlIgnore(path)
			if err != nil {
				t.Fatalf("LoadCrawlIgnore: %v", err)
			}

			got, err := m.Excludes(tt.input, tt.isDir)
			if err != nil {
				t.Fatalf("Excludes: %v", err)
			}
			if got != tt.want {
				t.Errorf("Excludes(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoadCrawlIgnore_MissingFile(t *testing.T) {
	_, err := LoadCrawlIgnore("/nonexistent/.crawlignore")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestLoadCrawlIgnore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, CrawlIgnoreFile)
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m, err := LoadCrawlIgnore(path)
	if err != nil {
		t.Fatalf("LoadCrawlIgnore: %v", err)
	}

	got, err := m.Excludes("anything", false)
	if err != nil {
		t.Fatalf("Excludes: %v", err)
	}
	if got {
		t.Error("empty crawlignore should not exclude anything")
	}
}

func TestLoadCrawlIgnore_OnlyComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, CrawlIgnoreFile)
	if err := os.WriteFile(path, []byte("# just a comment\n# another\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m, err := LoadCrawlIgnore(path)
	if err != nil {
		t.Fatalf("LoadCrawlIgnore: %v", err)
	}

	got, err := m.Excludes("anything", false)
	if err != nil {
		t.Fatalf("Excludes: %v", err)
	}
	if got {
		t.Error("comment-only crawlignore should not exclude anything")
	}
}
