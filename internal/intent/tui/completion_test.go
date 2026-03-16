package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// testShortcuts returns a standard set of shortcuts for testing.
func testShortcuts() map[string]string {
	return map[string]string{
		"p":  "projects/",
		"f":  "festivals/",
		"w":  "workflow/",
		"d":  "docs/",
		"a":  "ai_docs/",
		"de": "workflow/design/",
		"cr": "workflow/code_reviews/",
		"du": "dungeon/",
		"i":  ".campaign/intents/",
	}
}

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

func TestAtCompletionCandidates_EmptyQuery(t *testing.T) {
	sc := testShortcuts()
	candidates := atCompletionCandidates("", "/tmp/nonexistent", sc)

	// Should return unique paths sorted, capped at maxCompletionCandidates
	if len(candidates) == 0 {
		t.Fatal("expected candidates for empty query")
	}
	if len(candidates) > maxCompletionCandidates {
		t.Errorf("expected at most %d candidates, got %d", maxCompletionCandidates, len(candidates))
	}

	// Should be sorted
	for i := 1; i < len(candidates); i++ {
		if candidates[i-1] > candidates[i] {
			t.Errorf("candidates not sorted: %v", candidates)
			break
		}
	}

	// Should contain real paths, not shortcode keys
	found := false
	for _, c := range candidates {
		if c == "@docs/" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @docs/ in candidates, got %v", candidates)
	}
}

func TestAtCompletionCandidates_FuzzyMatchKey(t *testing.T) {
	sc := testShortcuts()

	// "de" should match shortcut key "de" → @workflow/design/
	candidates := atCompletionCandidates("de", "/tmp/nonexistent", sc)
	found := false
	for _, c := range candidates {
		if c == "@workflow/design/" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @workflow/design/ for query 'de', got %v", candidates)
	}
}

func TestAtCompletionCandidates_FuzzyMatchPath(t *testing.T) {
	sc := testShortcuts()

	// "design" should fuzzy-match path "workflow/design/"
	candidates := atCompletionCandidates("design", "/tmp/nonexistent", sc)
	found := false
	for _, c := range candidates {
		if c == "@workflow/design/" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @workflow/design/ for query 'design', got %v", candidates)
	}
}

func TestAtCompletionCandidates_NoMatch(t *testing.T) {
	sc := testShortcuts()
	candidates := atCompletionCandidates("zzz", "/tmp/nonexistent", sc)
	if len(candidates) != 0 {
		t.Errorf("expected no matches for 'zzz', got %v", candidates)
	}
}

func TestAtCompletionCandidates_FuzzyMatchMultiple(t *testing.T) {
	sc := testShortcuts()

	// "w" should match key "w" (workflow/) and "wt" would too if present
	candidates := atCompletionCandidates("w", "/tmp/nonexistent", sc)
	if len(candidates) == 0 {
		t.Fatal("expected candidates for 'w'")
	}

	// Should include @workflow/ (direct key match)
	found := false
	for _, c := range candidates {
		if c == "@workflow/" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @workflow/ in candidates for 'w', got %v", candidates)
	}
}

func TestAtCompletionCandidates_DirectoryListing(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	os.MkdirAll(filepath.Join(projectsDir, "fest"), 0755)
	os.MkdirAll(filepath.Join(projectsDir, "camp"), 0755)
	os.WriteFile(filepath.Join(projectsDir, "README.md"), []byte("hi"), 0644)

	sc := testShortcuts()

	// "projects/" should list directory contents (after shortcut expanded)
	candidates := atCompletionCandidates("projects/", root, sc)
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates for projects/, got %d: %v", len(candidates), candidates)
	}

	foundCamp := false
	foundFest := false
	for _, c := range candidates {
		if c == "@projects/camp/" {
			foundCamp = true
		}
		if c == "@projects/fest/" {
			foundFest = true
		}
	}
	if !foundCamp {
		t.Errorf("expected @projects/camp/ in candidates, got %v", candidates)
	}
	if !foundFest {
		t.Errorf("expected @projects/fest/ in candidates, got %v", candidates)
	}
}

func TestAtCompletionCandidates_FuzzyFilter(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	os.MkdirAll(filepath.Join(projectsDir, "fest"), 0755)
	os.MkdirAll(filepath.Join(projectsDir, "camp"), 0755)
	os.MkdirAll(filepath.Join(projectsDir, "obey-daemon"), 0755)

	sc := testShortcuts()

	// "projects/fe" should fuzzy-match fest within the projects/ directory
	candidates := atCompletionCandidates("projects/fe", root, sc)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate for projects/fe, got %d: %v", len(candidates), candidates)
	}
	if candidates[0] != "@projects/fest/" {
		t.Errorf("expected @projects/fest/, got %s", candidates[0])
	}
}

func TestAtCompletionCandidates_HiddenFilesExcluded(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	os.MkdirAll(filepath.Join(projectsDir, ".hidden"), 0755)
	os.MkdirAll(filepath.Join(projectsDir, "visible"), 0755)

	sc := testShortcuts()
	candidates := atCompletionCandidates("projects/", root, sc)
	for _, c := range candidates {
		if c == "@projects/.hidden/" {
			t.Error("hidden directory should be excluded from candidates")
		}
	}
}

func TestAtCompletionCandidates_NestedPath(t *testing.T) {
	root := t.TempDir()
	designDir := filepath.Join(root, "workflow", "design")
	os.MkdirAll(filepath.Join(designDir, "architecture"), 0755)
	os.MkdirAll(filepath.Join(designDir, "mockups"), 0755)

	sc := testShortcuts()

	// After expanding @de → @workflow/design/, further typing navigates the real path
	candidates := atCompletionCandidates("workflow/design/", root, sc)
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates for workflow/design/, got %d: %v", len(candidates), candidates)
	}

	foundArch := false
	for _, c := range candidates {
		if c == "@workflow/design/architecture/" {
			foundArch = true
		}
	}
	if !foundArch {
		t.Errorf("expected @workflow/design/architecture/ in candidates, got %v", candidates)
	}
}

func TestAtCompletionCandidates_NestedPathFilter(t *testing.T) {
	root := t.TempDir()
	designDir := filepath.Join(root, "workflow", "design")
	os.MkdirAll(filepath.Join(designDir, "architecture"), 0755)
	os.MkdirAll(filepath.Join(designDir, "mockups"), 0755)

	sc := testShortcuts()

	// "workflow/design/arch" should fuzzy-match architecture
	candidates := atCompletionCandidates("workflow/design/arch", root, sc)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate for workflow/design/arch, got %d: %v", len(candidates), candidates)
	}
	if candidates[0] != "@workflow/design/architecture/" {
		t.Errorf("expected @workflow/design/architecture/, got %s", candidates[0])
	}
}

func TestAtCompletionCandidates_DeduplicatesPaths(t *testing.T) {
	// Multiple shortcut keys can map to the same path
	sc := map[string]string{
		"d":    "docs/",
		"docs": "docs/",
	}
	candidates := atCompletionCandidates("", "/tmp/nonexistent", sc)
	count := 0
	for _, c := range candidates {
		if c == "@docs/" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected @docs/ exactly once, found %d times in %v", count, candidates)
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
		candidates: []string{"@projects/", "@workflow/", "@festivals/"},
		selected:   1,
	}
	view := completionView(cs)
	if view == "" {
		t.Error("completion view should not be empty with candidates")
	}
	if !containsText(view, "@projects/") {
		t.Error("view should contain @projects/")
	}
	if !containsText(view, "@workflow/") {
		t.Error("view should contain @workflow/")
	}
}
