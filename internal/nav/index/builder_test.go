package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/worktree"
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

// TestWorktreeTarget covers the pure classification of git worktree entries
// into navigation targets. The filesystem/git enumeration in scanWorktrees is
// exercised end-to-end in the containerized integration harness
// (tests/integration/navigation_worktree_test.go).
func TestWorktreeTarget(t *testing.T) {
	const (
		project     = "camp"
		projectPath = "/campaign/projects/camp"
	)

	tests := []struct {
		name     string
		entry    worktree.GitWorktreeEntry
		wantOK   bool
		wantName string
		wantPath string
	}{
		{
			name:   "bare entry skipped",
			entry:  worktree.GitWorktreeEntry{Path: "/campaign/projects/worktrees/camp/x", IsBare: true},
			wantOK: false,
		},
		{
			name:   "empty path skipped",
			entry:  worktree.GitWorktreeEntry{Path: ""},
			wantOK: false,
		},
		{
			name:   "main working tree skipped",
			entry:  worktree.GitWorktreeEntry{Path: projectPath, Branch: "main"},
			wantOK: false,
		},
		{
			name:   "submodule main worktree under .git skipped",
			entry:  worktree.GitWorktreeEntry{Path: "/campaign/.git/modules/projects/camp", Branch: "main"},
			wantOK: false,
		},
		{
			name:   "hidden worktree directory skipped",
			entry:  worktree.GitWorktreeEntry{Path: "/campaign/projects/worktrees/camp/.tmp"},
			wantOK: false,
		},
		{
			name:     "preferred-location worktree",
			entry:    worktree.GitWorktreeEntry{Path: "/campaign/projects/worktrees/camp/feature-auth", Branch: "feature-auth"},
			wantOK:   true,
			wantName: "camp@feature-auth",
			wantPath: "/campaign/projects/worktrees/camp/feature-auth",
		},
		{
			name:     "non-preferred-location worktree still resolves",
			entry:    worktree.GitWorktreeEntry{Path: "/campaign/projects/worktrees/fix-camp-392", IsDetached: true},
			wantOK:   true,
			wantName: "camp@fix-camp-392",
			wantPath: "/campaign/projects/worktrees/fix-camp-392",
		},
		{
			name:     "worktree entirely outside the worktrees dir still resolves",
			entry:    worktree.GitWorktreeEntry{Path: "/tmp/scratch/camp-experiment", Branch: "experiment"},
			wantOK:   true,
			wantName: "camp@camp-experiment",
			wantPath: "/tmp/scratch/camp-experiment",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := worktreeTarget(project, projectPath, tc.entry)
			if ok != tc.wantOK {
				t.Fatalf("worktreeTarget() ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if got.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", got.Path, tc.wantPath)
			}
			if got.Category != nav.CategoryWorktrees {
				t.Errorf("Category = %q, want %q", got.Category, nav.CategoryWorktrees)
			}
		})
	}
}

func TestBuilder_scanWorktrees_NoProjects(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	builder := NewBuilder(root)
	ctx := context.Background()

	targets, err := builder.scanWorktrees(ctx)
	if err != nil {
		t.Fatalf("scanWorktrees() error = %v", err)
	}

	if len(targets) != 0 {
		t.Errorf("Expected 0 targets when no projects are registered, got %d", len(targets))
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
