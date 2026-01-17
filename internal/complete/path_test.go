package complete

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/nav"
)

func TestPathComplete(t *testing.T) {
	// Create test campaign
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create projects
	for _, name := range []string{"api-service", "api-gateway", "web-app", "cli-tool"} {
		projPath := filepath.Join(root, "projects", name)
		if err := os.MkdirAll(projPath, 0755); err != nil {
			t.Fatalf("Failed to create project: %v", err)
		}
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	tests := []struct {
		name     string
		cat      nav.Category
		partial  string
		wantLen  int
		contains string
	}{
		{"api prefix", nav.CategoryProjects, "api", 2, "api-service"},
		{"api-s prefix", nav.CategoryProjects, "api-s", 1, "api-service"},
		{"web prefix", nav.CategoryProjects, "web", 1, "web-app"},
		{"no match", nav.CategoryProjects, "xyz", 0, ""},
		{"empty", nav.CategoryProjects, "", 4, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := PathComplete(ctx, tt.cat, tt.partial)
			if err != nil {
				t.Fatalf("PathComplete failed: %v", err)
			}

			if len(candidates) != tt.wantLen {
				t.Errorf("Got %d candidates, want %d", len(candidates), tt.wantLen)
			}

			if tt.contains != "" {
				found := false
				for _, c := range candidates {
					if c == tt.contains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected %q in candidates: %v", tt.contains, candidates)
				}
			}
		})
	}
}

func TestPathComplete_CaseInsensitive(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	projPath := filepath.Join(root, "projects", "ApiService")
	os.MkdirAll(projPath, 0755)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	candidates, err := PathComplete(ctx, nav.CategoryProjects, "api")
	if err != nil {
		t.Fatalf("PathComplete failed: %v", err)
	}

	if len(candidates) != 1 {
		t.Errorf("Expected 1 candidate for case-insensitive match, got %d", len(candidates))
	}
}

func TestCompleteWorktree_NoAt(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	// Create worktree structure
	worktreeDir := filepath.Join(root, "worktrees", "api-service", "feature-x")
	os.MkdirAll(worktreeDir, 0755)
	worktreeDir2 := filepath.Join(root, "worktrees", "api-service", "bugfix-y")
	os.MkdirAll(worktreeDir2, 0755)
	worktreeDir3 := filepath.Join(root, "worktrees", "web-app", "main")
	os.MkdirAll(worktreeDir3, 0755)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	// Test completing project names (without @)
	candidates, err := CompleteWorktree(ctx, "api")
	if err != nil {
		t.Fatalf("CompleteWorktree failed: %v", err)
	}

	// Should return project@
	found := false
	for _, c := range candidates {
		if c == "api-service@" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'api-service@' in candidates: %v", candidates)
	}
}

func TestCompleteWorktree_WithAt(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	// Create worktree structure
	worktreeDir := filepath.Join(root, "worktrees", "api-service", "feature-x")
	os.MkdirAll(worktreeDir, 0755)
	worktreeDir2 := filepath.Join(root, "worktrees", "api-service", "bugfix-y")
	os.MkdirAll(worktreeDir2, 0755)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	// Test completing with @
	candidates, err := CompleteWorktree(ctx, "api-service@")
	if err != nil {
		t.Fatalf("CompleteWorktree failed: %v", err)
	}

	if len(candidates) < 2 {
		t.Errorf("Expected at least 2 candidates for 'api-service@', got %d", len(candidates))
	}
}

func TestCompleteWorktree_WithBranchPartial(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	// Create worktree structure
	worktreeDir := filepath.Join(root, "worktrees", "api-service", "feature-x")
	os.MkdirAll(worktreeDir, 0755)
	worktreeDir2 := filepath.Join(root, "worktrees", "api-service", "bugfix-y")
	os.MkdirAll(worktreeDir2, 0755)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	candidates, err := CompleteWorktree(ctx, "api-service@feat")
	if err != nil {
		t.Fatalf("CompleteWorktree failed: %v", err)
	}

	if len(candidates) != 1 {
		t.Errorf("Expected 1 candidate for 'api-service@feat', got %d: %v", len(candidates), candidates)
	}
}

func TestCompleteFestival_Names(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	// Create festival structure
	activeFest := filepath.Join(root, "festivals", "active", "fest-improvements")
	os.MkdirAll(activeFest, 0755)
	plannedFest := filepath.Join(root, "festivals", "planned", "fest-v2")
	os.MkdirAll(plannedFest, 0755)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	candidates, err := CompleteFestival(ctx, "fest")
	if err != nil {
		t.Fatalf("CompleteFestival failed: %v", err)
	}

	if len(candidates) < 2 {
		t.Errorf("Expected at least 2 candidates, got %d: %v", len(candidates), candidates)
	}
}

func TestCompleteFestival_Phases(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	// Create festival with phases
	activeFest := filepath.Join(root, "festivals", "active", "fest-improvements", "001_CRITICAL")
	os.MkdirAll(activeFest, 0755)
	phase2 := filepath.Join(root, "festivals", "active", "fest-improvements", "002_FOUNDATION")
	os.MkdirAll(phase2, 0755)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	candidates, err := CompleteFestival(ctx, "fest-improvements/")
	if err != nil {
		t.Fatalf("CompleteFestival failed: %v", err)
	}

	if len(candidates) < 2 {
		t.Errorf("Expected at least 2 phase candidates, got %d: %v", len(candidates), candidates)
	}
}

func TestCompleteFestival_Sequences(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	// Create festival with sequences
	seq1 := filepath.Join(root, "festivals", "active", "fest-improvements", "001_CRITICAL", "01_fix_bugs")
	os.MkdirAll(seq1, 0755)
	seq2 := filepath.Join(root, "festivals", "active", "fest-improvements", "001_CRITICAL", "02_add_tests")
	os.MkdirAll(seq2, 0755)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	candidates, err := CompleteFestival(ctx, "fest-improvements/001_CRITICAL/")
	if err != nil {
		t.Fatalf("CompleteFestival failed: %v", err)
	}

	if len(candidates) < 2 {
		t.Errorf("Expected at least 2 sequence candidates, got %d: %v", len(candidates), candidates)
	}
}

func TestPathComplete_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := PathComplete(ctx, nav.CategoryProjects, "test")
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestCompleteWorktree_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CompleteWorktree(ctx, "test")
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestCompleteFestival_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CompleteFestival(ctx, "test")
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

// Benchmarks

func BenchmarkPathComplete(b *testing.B) {
	root := b.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	for i := 0; i < 50; i++ {
		name := filepath.Join(root, "projects", string(rune('a'+i%26))+"-project")
		os.MkdirAll(name, 0755)
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = PathComplete(ctx, nav.CategoryProjects, "a")
	}
}
