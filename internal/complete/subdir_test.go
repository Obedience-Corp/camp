package complete

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Obedience-Corp/camp/internal/nav"
)

func setupSubdirFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create a category directory: workflow/design/
	designDir := filepath.Join(root, "workflow", "design")

	// Create nested structure:
	// workflow/design/festival_app/
	// workflow/design/festival_app/src/
	// workflow/design/festival_app/src/components/
	// workflow/design/festival_app/docs/
	// workflow/design/festival_app/README.md (file)
	// workflow/design/other_project/
	// workflow/design/.hidden/
	dirs := []string{
		filepath.Join(designDir, "festival_app", "src", "components"),
		filepath.Join(designDir, "festival_app", "docs"),
		filepath.Join(designDir, "other_project"),
		filepath.Join(designDir, ".hidden"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create a file in festival_app
	if err := os.WriteFile(filepath.Join(designDir, "festival_app", "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestCompleteSubdirectory_TopLevelSlash(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	// "festival_app/" should list contents of festival_app
	candidates, err := CompleteSubdirectory(ctx, root, nav.CategoryDesign, "festival_app/")
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(candidates)
	expected := []string{"festival_app/README.md", "festival_app/docs/", "festival_app/src/"}
	if len(candidates) != len(expected) {
		t.Fatalf("got %d candidates %v, want %d %v", len(candidates), candidates, len(expected), expected)
	}
	for i, c := range candidates {
		if c != expected[i] {
			t.Errorf("candidate[%d] = %q, want %q", i, c, expected[i])
		}
	}
}

func TestCompleteSubdirectory_WithFilter(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	// "festival_app/s" should match "festival_app/src/"
	candidates, err := CompleteSubdirectory(ctx, root, nav.CategoryDesign, "festival_app/s")
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates %v, want 1", len(candidates), candidates)
	}
	if candidates[0] != "festival_app/src/" {
		t.Errorf("got %q, want %q", candidates[0], "festival_app/src/")
	}
}

func TestCompleteSubdirectory_DeepNesting(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	// "festival_app/src/" should list contents of src
	candidates, err := CompleteSubdirectory(ctx, root, nav.CategoryDesign, "festival_app/src/")
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates %v, want 1", len(candidates), candidates)
	}
	if candidates[0] != "festival_app/src/components/" {
		t.Errorf("got %q, want %q", candidates[0], "festival_app/src/components/")
	}
}

func TestCompleteSubdirectory_NoSlash(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	// No "/" in query - should return nil
	candidates, err := CompleteSubdirectory(ctx, root, nav.CategoryDesign, "festival_app")
	if err != nil {
		t.Fatal(err)
	}
	if candidates != nil {
		t.Errorf("expected nil, got %v", candidates)
	}
}

func TestCompleteSubdirectory_NonexistentDir(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	// Directory doesn't exist - should return nil
	candidates, err := CompleteSubdirectory(ctx, root, nav.CategoryDesign, "nonexistent/")
	if err != nil {
		t.Fatal(err)
	}
	if candidates != nil {
		t.Errorf("expected nil, got %v", candidates)
	}
}

func TestCompleteSubdirectory_HiddenFilesExcluded(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	// List all entries in design dir root — .hidden should not appear
	designDir := filepath.Join(root, "workflow", "design")
	entries, _ := os.ReadDir(designDir)
	hasHidden := false
	for _, e := range entries {
		if e.Name() == ".hidden" {
			hasHidden = true
		}
	}
	if !hasHidden {
		t.Fatal("test setup: .hidden directory should exist")
	}

	// Complete at category root level with empty query won't use this func,
	// but we can test via a parent that has .hidden
	// Create .hidden inside festival_app
	hiddenInApp := filepath.Join(root, "workflow", "design", "festival_app", ".gitignore_dir")
	os.MkdirAll(hiddenInApp, 0o755)

	candidates, err := CompleteSubdirectory(ctx, root, nav.CategoryDesign, "festival_app/.")
	if err != nil {
		t.Fatal(err)
	}
	// Should get no results since dot-prefixed entries are excluded
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for dot-prefix, got %v", candidates)
	}
}

func TestCompleteSubdirectory_CaseInsensitive(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	// "festival_app/S" should still match "src/" (case-insensitive)
	candidates, err := CompleteSubdirectory(ctx, root, nav.CategoryDesign, "festival_app/S")
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	if candidates[0] != "festival_app/src/" {
		t.Errorf("got %q, want %q", candidates[0], "festival_app/src/")
	}
}

func TestCompleteSubdirectory_ContextCancelled(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	candidates, err := CompleteSubdirectory(ctx, root, nav.CategoryDesign, "festival_app/")
	if err == nil {
		t.Error("expected context error")
	}
	if candidates != nil {
		t.Errorf("expected nil candidates, got %v", candidates)
	}
}

func TestCompleteSubdirectoryRich_Basic(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	candidates, err := CompleteSubdirectoryRich(ctx, root, nav.CategoryDesign, "festival_app/")
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3: %v", len(candidates), candidates)
	}

	// Verify candidates have category and path set
	for _, c := range candidates {
		if c.Category != string(nav.CategoryDesign) {
			t.Errorf("candidate %q: category = %q, want %q", c.Name, c.Category, nav.CategoryDesign)
		}
		if c.Path == "" {
			t.Errorf("candidate %q: path is empty", c.Name)
		}
	}
}

func TestCompleteSubdirectoryRich_NoSlash(t *testing.T) {
	root := setupSubdirFixture(t)
	ctx := context.Background()

	candidates, err := CompleteSubdirectoryRich(ctx, root, nav.CategoryDesign, "festival_app")
	if err != nil {
		t.Fatal(err)
	}
	if candidates != nil {
		t.Errorf("expected nil, got %v", candidates)
	}
}

func TestCategoryAbsDir(t *testing.T) {
	root := "/campaign"

	tests := []struct {
		cat  nav.Category
		want string
	}{
		{nav.CategoryDesign, "/campaign/workflow/design"},
		{nav.CategoryProjects, "/campaign/projects"},
		{nav.CategoryFestivals, "/campaign/festivals"},
		{nav.CategoryAll, "/campaign"},
		{nav.Category(""), "/campaign"},
	}

	for _, tt := range tests {
		got := categoryAbsDir(root, tt.cat)
		if got != tt.want {
			t.Errorf("categoryAbsDir(%q, %q) = %q, want %q", root, tt.cat, got, tt.want)
		}
	}
}
