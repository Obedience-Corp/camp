package dungeon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestListDocsSubdirectories(t *testing.T) {
	root, err := os.MkdirTemp("", "dungeon-triage-docs-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	dirs := []string{
		filepath.Join(root, "docs", "architecture"),
		filepath.Join(root, "docs", "architecture", "api"),
		filepath.Join(root, "docs", "guides"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}

	got, err := listDocsSubdirectories(root)
	if err != nil {
		t.Fatalf("listDocsSubdirectories failed: %v", err)
	}

	want := []string{"architecture", "architecture/api", "guides"}
	if len(got) != len(want) {
		t.Fatalf("listDocsSubdirectories len=%d, want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("listDocsSubdirectories[%d]=%q, want=%q (%v)", i, got[i], want[i], got)
		}
	}
}

func TestListDocsSubdirectories_RequiresDocsRoot(t *testing.T) {
	root, err := os.MkdirTemp("", "dungeon-triage-docs-missing-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	_, err = listDocsSubdirectories(root)
	if err == nil {
		t.Fatal("expected missing docs root error")
	}
	if !errors.Is(err, ErrInvalidDocsDestination) {
		t.Fatalf("expected ErrInvalidDocsDestination, got: %v", err)
	}
}

func TestDocsMoveSummaryKey(t *testing.T) {
	root := "/tmp/campaign"
	target := filepath.Join(root, "docs", "architecture", "api", "note.md")
	got := docsMoveSummaryKey(root, target)
	want := "docs/architecture/api"
	if got != want {
		t.Fatalf("docsMoveSummaryKey() = %q, want %q", got, want)
	}
}

func TestDocsLevelSuggestions(t *testing.T) {
	root, err := os.MkdirTemp("", "dungeon-level-suggestions-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	// Create structure:
	// root/adr/          (leaf - no child dirs)
	// root/business/articles/  (business has children)
	// root/business/pricing/   (business has children)
	// root/guides/             (leaf - no child dirs)
	dirs := []string{
		filepath.Join(root, "adr"),
		filepath.Join(root, "business", "articles"),
		filepath.Join(root, "business", "pricing"),
		filepath.Join(root, "guides"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}

	t.Run("top level", func(t *testing.T) {
		got := docsLevelSuggestions(root, "")
		want := []string{"adr", "business/", "guides"}
		assertStringSlice(t, got, want)
	})

	t.Run("drill into business", func(t *testing.T) {
		got := docsLevelSuggestions(root, "business/")
		want := []string{"business/articles", "business/pricing"}
		assertStringSlice(t, got, want)
	})

	t.Run("nonexistent prefix", func(t *testing.T) {
		got := docsLevelSuggestions(root, "nonexistent/")
		if got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
}

func TestDocsParentPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"architecture/", ""},
		{"business/case-studies/", "business/"},
		{"a/b/c/", "a/b/"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := docsParentPrefix(tt.input)
			if got != tt.want {
				t.Fatalf("docsParentPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len=%d, want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d]=%q, want=%q (%v)", i, got[i], want[i], got)
		}
	}
}

func TestAppendDocsSuggestion(t *testing.T) {
	got := appendDocsSuggestion([]string{"guides"}, "architecture/api")
	got = appendDocsSuggestion(got, "guides")
	got = appendDocsSuggestion(got, "")

	want := []string{"architecture/api", "guides"}
	if len(got) != len(want) {
		t.Fatalf("appendDocsSuggestion len=%d, want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("appendDocsSuggestion[%d]=%q, want=%q (%v)", i, got[i], want[i], got)
		}
	}
}
