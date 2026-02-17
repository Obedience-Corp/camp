package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/obediencecorp/camp/internal/nav"
)

func TestNewBuilder(t *testing.T) {
	builder := NewBuilder("/test/root")
	if builder == nil {
		t.Fatal("NewBuilder returned nil")
	}
	if builder.root != "/test/root" {
		t.Errorf("root = %q, want %q", builder.root, "/test/root")
	}
}

func TestBuilder_Build(t *testing.T) {
	// Create temp directory structure
	root := setupTestCampaign(t)

	builder := NewBuilder(root)
	ctx := context.Background()

	idx, err := builder.Build(ctx)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if idx == nil {
		t.Fatal("Build() returned nil index")
	}

	if idx.CampaignRoot != root {
		t.Errorf("CampaignRoot = %q, want %q", idx.CampaignRoot, root)
	}

	if idx.Version != IndexVersion {
		t.Errorf("Version = %d, want %d", idx.Version, IndexVersion)
	}

	if idx.BuildTime.IsZero() {
		t.Error("BuildTime should not be zero")
	}

	// Should have found our test targets
	if len(idx.Targets) == 0 {
		t.Error("Expected some targets to be found")
	}
}

func TestBuilder_Build_EmptyRoot(t *testing.T) {
	root := t.TempDir()
	// Resolve symlinks for macOS
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("Failed to resolve symlinks: %v", err)
	}

	builder := NewBuilder(root)
	ctx := context.Background()

	idx, err := builder.Build(ctx)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if idx == nil {
		t.Fatal("Build() returned nil index")
	}

	// Empty campaign should have no targets
	if len(idx.Targets) != 0 {
		t.Errorf("Expected 0 targets for empty root, got %d", len(idx.Targets))
	}
}

func TestBuilder_Build_ContextCancellation(t *testing.T) {
	root := setupTestCampaign(t)

	builder := NewBuilder(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := builder.Build(ctx)
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestBuilder_Build_ContextTimeout(t *testing.T) {
	root := setupTestCampaign(t)

	builder := NewBuilder(root)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Small delay to ensure timeout
	time.Sleep(time.Millisecond)

	_, err := builder.Build(ctx)
	if err == nil {
		t.Error("Expected error for timed out context")
	}
}

func TestBuilder_scanCategory(t *testing.T) {
	root := setupTestCampaign(t)

	builder := NewBuilder(root)
	ctx := context.Background()

	targets, err := builder.scanCategory(ctx, nav.CategoryProjects)
	if err != nil {
		t.Fatalf("scanCategory() error = %v", err)
	}

	// Should find our test projects
	if len(targets) != 2 {
		t.Errorf("Expected 2 project targets, got %d", len(targets))
	}

	// Verify target properties
	for _, target := range targets {
		if target.Category != nav.CategoryProjects {
			t.Errorf("Target category = %q, want %q", target.Category, nav.CategoryProjects)
		}
		if target.Name == "" {
			t.Error("Target name should not be empty")
		}
		if target.Path == "" {
			t.Error("Target path should not be empty")
		}
	}
}

func TestBuilder_scanCategory_NonExistent(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	builder := NewBuilder(root)
	ctx := context.Background()

	targets, err := builder.scanCategory(ctx, nav.CategoryProjects)
	if err != nil {
		t.Fatalf("scanCategory() error = %v", err)
	}

	// Non-existent directory should return empty, not error
	if len(targets) != 0 {
		t.Errorf("Expected 0 targets for non-existent category, got %d", len(targets))
	}
}

func TestBuilder_scanCategory_SkipsHiddenFiles(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create projects directory with hidden and visible entries
	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(filepath.Join(projectsDir, ".hidden-project"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectsDir, "visible-project"), 0755); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(root)
	ctx := context.Background()

	targets, err := builder.scanCategory(ctx, nav.CategoryProjects)
	if err != nil {
		t.Fatalf("scanCategory() error = %v", err)
	}

	if len(targets) != 1 {
		t.Errorf("Expected 1 target (hidden skipped), got %d", len(targets))
	}

	if len(targets) > 0 && targets[0].Name != "visible-project" {
		t.Errorf("Expected visible-project, got %s", targets[0].Name)
	}
}

func TestBuilder_scanCategory_SkipsFiles(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create projects directory with both files and directories
	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectsDir, "real-project"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectsDir, "README.md"), []byte("# README"), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(root)
	ctx := context.Background()

	targets, err := builder.scanCategory(ctx, nav.CategoryProjects)
	if err != nil {
		t.Fatalf("scanCategory() error = %v", err)
	}

	// Projects should only include directory, not file
	if len(targets) != 1 {
		t.Errorf("Expected 1 project target (file skipped), got %d", len(targets))
	}
}

func TestBuilder_scanWorktrees(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create worktrees directory structure: projects/worktrees/<project>/<branch>
	worktreesDir := filepath.Join(root, "projects", "worktrees")
	if err := os.MkdirAll(filepath.Join(worktreesDir, "myproject", "feature-branch"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktreesDir, "myproject", "main"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktreesDir, "other-proj", "dev"), 0755); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(root)
	ctx := context.Background()

	targets, err := builder.scanWorktrees(ctx)
	if err != nil {
		t.Fatalf("scanWorktrees() error = %v", err)
	}

	if len(targets) != 3 {
		t.Errorf("Expected 3 worktree targets, got %d", len(targets))
	}

	// Verify naming convention: project@branch
	names := make(map[string]bool)
	for _, target := range targets {
		names[target.Name] = true
		if target.Category != nav.CategoryWorktrees {
			t.Errorf("Worktree category = %q, want %q", target.Category, nav.CategoryWorktrees)
		}
	}

	if !names["myproject@feature-branch"] {
		t.Error("Expected myproject@feature-branch in targets")
	}
	if !names["myproject@main"] {
		t.Error("Expected myproject@main in targets")
	}
	if !names["other-proj@dev"] {
		t.Error("Expected other-proj@dev in targets")
	}
}

func TestBuilder_scanWorktrees_SkipsHidden(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	worktreesDir := filepath.Join(root, "projects", "worktrees")
	if err := os.MkdirAll(filepath.Join(worktreesDir, ".hidden-proj", "main"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktreesDir, "visible-proj", ".hidden-branch"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktreesDir, "visible-proj", "visible-branch"), 0755); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(root)
	ctx := context.Background()

	targets, err := builder.scanWorktrees(ctx)
	if err != nil {
		t.Fatalf("scanWorktrees() error = %v", err)
	}

	if len(targets) != 1 {
		t.Errorf("Expected 1 target (hidden skipped), got %d", len(targets))
	}

	if len(targets) > 0 && targets[0].Name != "visible-proj@visible-branch" {
		t.Errorf("Expected visible-proj@visible-branch, got %s", targets[0].Name)
	}
}

func TestBuilder_scanWorktrees_NoWorktreesDir(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	builder := NewBuilder(root)
	ctx := context.Background()

	targets, err := builder.scanWorktrees(ctx)
	if err != nil {
		t.Fatalf("scanWorktrees() error = %v", err)
	}

	if len(targets) != 0 {
		t.Errorf("Expected 0 targets for missing worktrees dir, got %d", len(targets))
	}
}

func TestBuilder_BuildWithOptions(t *testing.T) {
	root := setupTestCampaign(t)

	builder := NewBuilder(root)
	ctx := context.Background()

	// Test with specific categories
	opts := BuildOptions{
		Categories: []nav.Category{nav.CategoryProjects},
	}

	idx, err := builder.BuildWithOptions(ctx, opts)
	if err != nil {
		t.Fatalf("BuildWithOptions() error = %v", err)
	}

	// Should only have projects
	for _, target := range idx.Targets {
		if target.Category != nav.CategoryProjects {
			t.Errorf("Expected only projects, got %s", target.Category)
		}
	}
}

func TestBuilder_BuildWithOptions_IncludeHidden(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(filepath.Join(projectsDir, ".hidden-project"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectsDir, "visible-project"), 0755); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(root)
	ctx := context.Background()

	// Without IncludeHidden
	opts := BuildOptions{Categories: []nav.Category{nav.CategoryProjects}}
	idx, _ := builder.BuildWithOptions(ctx, opts)
	if len(idx.Targets) != 1 {
		t.Errorf("Without IncludeHidden: expected 1 target, got %d", len(idx.Targets))
	}

	// With IncludeHidden
	opts.IncludeHidden = true
	idx, _ = builder.BuildWithOptions(ctx, opts)
	if len(idx.Targets) != 2 {
		t.Errorf("With IncludeHidden: expected 2 targets, got %d", len(idx.Targets))
	}
}

func TestBuilder_BuildWithOptions_ContextCancellation(t *testing.T) {
	root := setupTestCampaign(t)

	builder := NewBuilder(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := builder.BuildWithOptions(ctx, BuildOptions{})
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestContainsCategory(t *testing.T) {
	cats := []nav.Category{nav.CategoryProjects, nav.CategoryFestivals}

	if !containsCategory(cats, nav.CategoryProjects) {
		t.Error("Should contain CategoryProjects")
	}

	if !containsCategory(cats, nav.CategoryFestivals) {
		t.Error("Should contain CategoryFestivals")
	}

	if containsCategory(cats, nav.CategoryDocs) {
		t.Error("Should not contain CategoryDocs")
	}

	if containsCategory(nil, nav.CategoryProjects) {
		t.Error("Empty slice should not contain anything")
	}
}

// setupTestCampaign creates a test campaign directory structure.
func setupTestCampaign(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	// Resolve symlinks for macOS (/var -> /private/var)
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("Failed to resolve symlinks: %v", err)
	}

	// Create campaign marker
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create category directories with entries
	dirs := map[string][]string{
		"projects":  {"api-service", "web-app"},
		"festivals": {"camp-cli"},
		"docs":      {"architecture"},
		"workflow":  {"code_reviews"},
	}

	for cat, entries := range dirs {
		catDir := filepath.Join(root, cat)
		if err := os.MkdirAll(catDir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if err := os.MkdirAll(filepath.Join(catDir, entry), 0755); err != nil {
				t.Fatal(err)
			}
		}
	}

	return root
}

func TestBuilder_Build_SkipsDungeonCategory(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create a dungeon category with entries
	dungeonDir := filepath.Join(root, "dungeon")
	if err := os.MkdirAll(filepath.Join(dungeonDir, "old-project"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dungeonDir, "deprecated-lib"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a non-dungeon category for comparison
	if err := os.MkdirAll(filepath.Join(root, "projects", "active-project"), 0755); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(root)
	idx, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Verify no dungeon targets were indexed
	for _, target := range idx.Targets {
		if target.Category == nav.CategoryDungeon {
			t.Errorf("Dungeon target %q should not be in index", target.Name)
		}
	}

	// Verify non-dungeon targets still work
	found := false
	for _, target := range idx.Targets {
		if target.Name == "active-project" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected active-project in index")
	}
}

func TestBuilder_scanCategory_SkipsDungeonSubdirs(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create festivals with a dungeon subdirectory alongside real entries
	festivalsDir := filepath.Join(root, "festivals")
	if err := os.MkdirAll(filepath.Join(festivalsDir, "camp-cli"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(festivalsDir, "dungeon", "old-fest"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(festivalsDir, "another-fest"), 0755); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(root)
	targets, err := builder.scanCategory(context.Background(), nav.CategoryFestivals)
	if err != nil {
		t.Fatalf("scanCategory() error = %v", err)
	}

	// Should have 2 targets: camp-cli and another-fest (not dungeon)
	if len(targets) != 2 {
		t.Errorf("Expected 2 targets (dungeon excluded), got %d", len(targets))
	}

	for _, target := range targets {
		if target.Name == "dungeon" {
			t.Error("Dungeon subdirectory should be excluded from category scan")
		}
	}
}

// Benchmarks

func BenchmarkBuilder_Build(b *testing.B) {
	root := b.TempDir()

	// Create a realistic campaign structure
	dirs := []string{"projects", "festivals", "docs", "workflow"}
	for _, dir := range dirs {
		catDir := filepath.Join(root, dir)
		if err := os.MkdirAll(catDir, 0755); err != nil {
			b.Fatal(err)
		}
		// Add 20 entries per category
		for i := 0; i < 20; i++ {
			entryDir := filepath.Join(catDir, "entry-"+string(rune('a'+i)))
			if err := os.MkdirAll(entryDir, 0755); err != nil {
				b.Fatal(err)
			}
		}
	}

	builder := NewBuilder(root)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := builder.Build(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuilder_scanCategory(b *testing.B) {
	root := b.TempDir()

	// Create projects directory with many entries
	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		entryDir := filepath.Join(projectsDir, "project-"+string(rune('a'+i%26))+string(rune('0'+i/26)))
		if err := os.MkdirAll(entryDir, 0755); err != nil {
			b.Fatal(err)
		}
	}

	builder := NewBuilder(root)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := builder.scanCategory(ctx, nav.CategoryProjects)
		if err != nil {
			b.Fatal(err)
		}
	}
}
