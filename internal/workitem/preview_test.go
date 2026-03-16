package workitem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHumanizeBasename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"camp-workitem-dashboard", "Camp Workitem Dashboard"},
		{"simple", "Simple"},
		{"multi_word_name", "Multi Word Name"},
		{"already Good", "Already Good"},
		{"MixedCase", "MixedCase"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := humanizeBasename(tt.input)
			if got != tt.want {
				t.Errorf("humanizeBasename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractSummary(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		want   string
	}{
		{
			name:   "empty",
			text:   "",
			maxLen: 100,
			want:   "",
		},
		{
			name:   "skips headings",
			text:   "# Title\n\nThis is the body text.",
			maxLen: 100,
			want:   "This is the body text.",
		},
		{
			name:   "truncates long text",
			text:   "This is a longer body text that should be truncated at some reasonable word boundary for display.",
			maxLen: 40,
		},
		{
			name:   "skips frontmatter completely",
			text:   "---\ntitle: foo\nstatus: active\n---\n\nReal content here.",
			maxLen: 100,
			want:   "Real content here.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSummary(tt.text, tt.maxLen)
			if tt.want != "" && got != tt.want {
				t.Errorf("extractSummary() = %q, want %q", got, tt.want)
			}
			if tt.maxLen > 0 && len(got) > tt.maxLen+3 { // +3 for "..."
				t.Errorf("extractSummary() len=%d exceeds maxLen=%d", len(got), tt.maxLen)
			}
		})
	}
}

func TestFindPrimaryDoc(t *testing.T) {
	dir := t.TempDir()

	t.Run("returns README.md when present", func(t *testing.T) {
		readme := filepath.Join(dir, "README.md")
		os.WriteFile(readme, []byte("# Test"), 0644)
		defer os.Remove(readme)

		got := findPrimaryDoc(dir)
		if filepath.Base(got) != "README.md" {
			t.Errorf("findPrimaryDoc() = %q, want README.md", got)
		}
	})

	t.Run("returns first md when no README", func(t *testing.T) {
		subdir := filepath.Join(dir, "noreadme")
		os.MkdirAll(subdir, 0755)
		os.WriteFile(filepath.Join(subdir, "SPEC.md"), []byte("# Spec"), 0644)
		os.WriteFile(filepath.Join(subdir, "other.txt"), []byte("not md"), 0644)

		got := findPrimaryDoc(subdir)
		if filepath.Base(got) != "SPEC.md" {
			t.Errorf("findPrimaryDoc() = %q, want SPEC.md", got)
		}
	})

	t.Run("returns empty for no md files", func(t *testing.T) {
		subdir := filepath.Join(dir, "nomd")
		os.MkdirAll(subdir, 0755)
		os.WriteFile(filepath.Join(subdir, "data.json"), []byte("{}"), 0644)

		got := findPrimaryDoc(subdir)
		if got != "" {
			t.Errorf("findPrimaryDoc() = %q, want empty", got)
		}
	})
}

func TestExtractFirstHeading(t *testing.T) {
	dir := t.TempDir()

	t.Run("extracts heading after frontmatter", func(t *testing.T) {
		f := filepath.Join(dir, "with_fm.md")
		os.WriteFile(f, []byte("---\ntitle: foo\n---\n\n# My Heading\n\nBody."), 0644)

		got := extractFirstHeading(f)
		if got != "My Heading" {
			t.Errorf("extractFirstHeading() = %q, want 'My Heading'", got)
		}
	})

	t.Run("extracts heading without frontmatter", func(t *testing.T) {
		f := filepath.Join(dir, "no_fm.md")
		os.WriteFile(f, []byte("# Direct Heading\n\nBody."), 0644)

		got := extractFirstHeading(f)
		if got != "Direct Heading" {
			t.Errorf("extractFirstHeading() = %q, want 'Direct Heading'", got)
		}
	})

	t.Run("returns empty when no heading", func(t *testing.T) {
		f := filepath.Join(dir, "no_heading.md")
		os.WriteFile(f, []byte("Just body text."), 0644)

		got := extractFirstHeading(f)
		if got != "" {
			t.Errorf("extractFirstHeading() = %q, want empty", got)
		}
	})
}
