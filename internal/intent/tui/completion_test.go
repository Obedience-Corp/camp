package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractAtQuery(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		cursorCol int
		wantQuery string
		wantCol   int
	}{
		{
			name:      "no @ in line",
			line:      "hello world",
			cursorCol: 5,
			wantQuery: "",
			wantCol:   -1,
		},
		{
			name:      "cursor right after @",
			line:      "see @",
			cursorCol: 5,
			wantQuery: "",
			wantCol:   4,
		},
		{
			name:      "partial prefix",
			line:      "see @p",
			cursorCol: 6,
			wantQuery: "p",
			wantCol:   4,
		},
		{
			name:      "full prefix with slash",
			line:      "see @p/",
			cursorCol: 7,
			wantQuery: "p/",
			wantCol:   4,
		},
		{
			name:      "prefix with partial name",
			line:      "see @p/fest",
			cursorCol: 11,
			wantQuery: "p/fest",
			wantCol:   4,
		},
		{
			name:      "@ at start of line",
			line:      "@w/design",
			cursorCol: 9,
			wantQuery: "w/design",
			wantCol:   0,
		},
		{
			name:      "cursor not in @ word",
			line:      "@p/test and more text",
			cursorCol: 18,
			wantQuery: "",
			wantCol:   -1,
		},
		{
			name:      "@ mid-sentence",
			line:      "use @f/ for festivals",
			cursorCol: 7,
			wantQuery: "f/",
			wantCol:   4,
		},
		{
			name:      "cursor at 0",
			line:      "hello",
			cursorCol: 0,
			wantQuery: "",
			wantCol:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, col := extractAtQuery(tt.line, tt.cursorCol)
			if query != tt.wantQuery {
				t.Errorf("query = %q, want %q", query, tt.wantQuery)
			}
			if col != tt.wantCol {
				t.Errorf("atCol = %d, want %d", col, tt.wantCol)
			}
		})
	}
}

func TestFuzzyContains(t *testing.T) {
	tests := []struct {
		haystack string
		needle   string
		want     bool
	}{
		{"projects", "p", true},
		{"projects", "prj", true},
		{"festivals", "fest", true},
		{"festivals", "fl", true},
		{"projects", "xyz", false},
		{"abc", "abcd", false},
		{"Projects", "proj", true}, // case insensitive
		{"", "", true},
		{"abc", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.haystack+"/"+tt.needle, func(t *testing.T) {
			got := fuzzyContains(tt.haystack, tt.needle)
			if got != tt.want {
				t.Errorf("fuzzyContains(%q, %q) = %v, want %v", tt.haystack, tt.needle, got, tt.want)
			}
		})
	}
}

func TestAtCompletionCandidates_TopLevel(t *testing.T) {
	// Empty query should return top-level shortcuts
	candidates := atCompletionCandidates("", "/tmp/nonexistent")
	if len(candidates) != 4 {
		t.Fatalf("expected 4 top-level candidates, got %d: %v", len(candidates), candidates)
	}

	// Should be sorted
	for i := 1; i < len(candidates); i++ {
		if candidates[i-1] > candidates[i] {
			t.Errorf("candidates not sorted: %v", candidates)
			break
		}
	}
}

func TestAtCompletionCandidates_FuzzyTopLevel(t *testing.T) {
	// "p" should match @p/
	candidates := atCompletionCandidates("z", "/tmp/nonexistent")
	if len(candidates) != 0 {
		t.Errorf("expected no matches for 'z', got %v", candidates)
	}

	candidates = atCompletionCandidates("w", "/tmp/nonexistent")
	// "w" matches @w/
	found := false
	for _, c := range candidates {
		if c == "@w/" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @w/ in candidates for 'w', got %v", candidates)
	}
}

func TestAtCompletionCandidates_DirectoryListing(t *testing.T) {
	// Create a temp campaign directory structure
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	os.MkdirAll(filepath.Join(projectsDir, "fest"), 0755)
	os.MkdirAll(filepath.Join(projectsDir, "camp"), 0755)
	os.WriteFile(filepath.Join(projectsDir, "README.md"), []byte("hi"), 0644)

	// @p/ should list directory contents
	candidates := atCompletionCandidates("p/", root)
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates for @p/, got %d: %v", len(candidates), candidates)
	}

	// Should contain camp/ and fest/ as directories
	foundCamp := false
	foundFest := false
	for _, c := range candidates {
		if c == "@p/camp/" {
			foundCamp = true
		}
		if c == "@p/fest/" {
			foundFest = true
		}
	}
	if !foundCamp {
		t.Errorf("expected @p/camp/ in candidates, got %v", candidates)
	}
	if !foundFest {
		t.Errorf("expected @p/fest/ in candidates, got %v", candidates)
	}
}

func TestAtCompletionCandidates_FuzzyFilter(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	os.MkdirAll(filepath.Join(projectsDir, "fest"), 0755)
	os.MkdirAll(filepath.Join(projectsDir, "camp"), 0755)
	os.MkdirAll(filepath.Join(projectsDir, "obey-daemon"), 0755)

	// @p/fe should fuzzy-match fest
	candidates := atCompletionCandidates("p/fe", root)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate for @p/fe, got %d: %v", len(candidates), candidates)
	}
	if candidates[0] != "@p/fest/" {
		t.Errorf("expected @p/fest/, got %s", candidates[0])
	}
}

func TestAtCompletionCandidates_HiddenFilesExcluded(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	os.MkdirAll(filepath.Join(projectsDir, ".hidden"), 0755)
	os.MkdirAll(filepath.Join(projectsDir, "visible"), 0755)

	candidates := atCompletionCandidates("p/", root)
	for _, c := range candidates {
		if c == "@p/.hidden/" {
			t.Error("hidden directory should be excluded from candidates")
		}
	}
}

func TestCompletionView_Empty(t *testing.T) {
	cs := &completionState{active: false}
	if got := completionView(cs); got != "" {
		t.Errorf("inactive completion should render empty, got %q", got)
	}
}

func TestCompletionView_WithCandidates(t *testing.T) {
	cs := &completionState{
		active:     true,
		candidates: []string{"@p/", "@w/", "@f/"},
		selected:   1,
	}
	view := completionView(cs)
	if view == "" {
		t.Error("completion view should not be empty with candidates")
	}
	if !containsText(view, "@p/") {
		t.Error("view should contain @p/")
	}
	if !containsText(view, "@w/") {
		t.Error("view should contain @w/")
	}
}
