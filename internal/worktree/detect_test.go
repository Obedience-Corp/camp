package worktree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/paths"
)

func newTestDetector(t *testing.T, root string) *Detector {
	t.Helper()
	resolver := paths.NewResolver(root, config.DefaultCampaignPaths())
	return NewDetector(resolver)
}

func TestDetector_DetectFromPath(t *testing.T) {
	tmpDir := t.TempDir()
	detector := newTestDetector(t, tmpDir)

	// Create fake worktree structure
	wtPath := filepath.Join(tmpDir, "projects", "worktrees", "my-api", "feature")
	if err := os.MkdirAll(wtPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create .git file (simulating worktree)
	gitFile := filepath.Join(wtPath, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /path/to/gitdir"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, err := detector.DetectFromPath(wtPath)
	if err != nil {
		t.Fatalf("DetectFromPath() error = %v", err)
	}

	if ctx.Project != "my-api" {
		t.Errorf("Project = %q, want my-api", ctx.Project)
	}
	if ctx.WorktreeName != "feature" {
		t.Errorf("WorktreeName = %q, want feature", ctx.WorktreeName)
	}
	if ctx.WorktreePath != wtPath {
		t.Errorf("WorktreePath = %q, want %q", ctx.WorktreePath, wtPath)
	}
}

func TestDetector_DetectFromPath_Nested(t *testing.T) {
	tmpDir := t.TempDir()
	detector := newTestDetector(t, tmpDir)

	// Create fake worktree structure
	wtPath := filepath.Join(tmpDir, "projects", "worktrees", "my-api", "feature")
	srcPath := filepath.Join(wtPath, "src", "main")
	if err := os.MkdirAll(srcPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create .git file (simulating worktree)
	gitFile := filepath.Join(wtPath, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /path/to/gitdir"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, err := detector.DetectFromPath(srcPath)
	if err != nil {
		t.Fatalf("DetectFromPath() error = %v", err)
	}

	if ctx.Project != "my-api" {
		t.Errorf("Project = %q, want my-api", ctx.Project)
	}
	if ctx.WorktreeName != "feature" {
		t.Errorf("WorktreeName = %q, want feature", ctx.WorktreeName)
	}
}

func TestDetector_DetectFromPath_NotWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	detector := newTestDetector(t, tmpDir)

	_, err := detector.DetectFromPath(tmpDir)
	if err == nil {
		t.Error("expected error for non-worktree path")
	}
}

func TestDetector_DetectFromPath_RegularGitDir(t *testing.T) {
	tmpDir := t.TempDir()
	detector := newTestDetector(t, tmpDir)

	// Create fake worktree structure with .git as directory (not worktree)
	wtPath := filepath.Join(tmpDir, "projects", "worktrees", "my-api", "feature")
	gitDir := filepath.Join(wtPath, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := detector.DetectFromPath(wtPath)
	if err == nil {
		t.Error("expected error for .git directory (not worktree)")
	}
}

func TestDetector_IsInWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	detector := newTestDetector(t, tmpDir)

	// Create fake worktree structure
	wtPath := filepath.Join(tmpDir, "projects", "worktrees", "my-api", "feature")
	if err := os.MkdirAll(wtPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create .git file (simulating worktree)
	gitFile := filepath.Join(wtPath, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /path/to/gitdir"), 0644); err != nil {
		t.Fatal(err)
	}

	if !detector.IsInWorktree(wtPath) {
		t.Error("IsInWorktree() = false for valid worktree")
	}

	if detector.IsInWorktree(tmpDir) {
		t.Error("IsInWorktree() = true for non-worktree")
	}
}

func TestDetector_FindWorktreeRoot(t *testing.T) {
	tmpDir := t.TempDir()
	detector := newTestDetector(t, tmpDir)

	// Create fake worktree structure
	wtPath := filepath.Join(tmpDir, "projects", "worktrees", "my-api", "feature")
	srcPath := filepath.Join(wtPath, "src", "pkg")
	if err := os.MkdirAll(srcPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create .git file (simulating worktree)
	gitFile := filepath.Join(wtPath, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /path/to/gitdir"), 0644); err != nil {
		t.Fatal(err)
	}

	root, err := detector.FindWorktreeRoot(srcPath)
	if err != nil {
		t.Fatalf("FindWorktreeRoot() error = %v", err)
	}

	if root != wtPath {
		t.Errorf("FindWorktreeRoot() = %q, want %q", root, wtPath)
	}
}
