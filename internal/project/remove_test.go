package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/pathutil"
)

func TestRemove_ProjectNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create projects directory but no projects
	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	ctx := context.Background()
	_, err := Remove(ctx, tmpDir, "nonexistent", RemoveOptions{})

	if err == nil {
		t.Fatal("Remove() should return error for nonexistent project")
	}

	var notFound *ErrProjectNotFound
	if _, ok := err.(*ErrProjectNotFound); !ok {
		t.Errorf("error type = %T, want *ErrProjectNotFound", err)
	} else {
		notFound = err.(*ErrProjectNotFound)
		if notFound.Name != "nonexistent" {
			t.Errorf("ErrProjectNotFound.Name = %q, want %q", notFound.Name, "nonexistent")
		}
	}
}

func TestRemove_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create projects directory with a project
	projectsDir := filepath.Join(tmpDir, "projects")
	projectPath := filepath.Join(projectsDir, "test-project")
	os.MkdirAll(projectPath, 0755)
	os.WriteFile(filepath.Join(projectPath, "file.txt"), []byte("test"), 0644)

	ctx := context.Background()
	result, err := Remove(ctx, tmpDir, "test-project", RemoveOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Check result
	if !result.SubmoduleRemoved {
		t.Error("DryRun should report SubmoduleRemoved = true")
	}

	// Verify files still exist
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Error("DryRun should not delete files")
	}
}

func TestRemove_DryRunWithDelete(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create projects and worktrees
	projectsDir := filepath.Join(tmpDir, "projects")
	projectPath := filepath.Join(projectsDir, "test-project")
	os.MkdirAll(projectPath, 0755)

	worktreesDir := filepath.Join(tmpDir, "worktrees")
	worktreePath := filepath.Join(worktreesDir, "test-project")
	os.MkdirAll(worktreePath, 0755)

	ctx := context.Background()
	result, err := Remove(ctx, tmpDir, "test-project", RemoveOptions{
		Delete: true,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Check result
	if !result.FilesDeleted {
		t.Error("DryRun with Delete should report FilesDeleted = true")
	}
	if !result.WorktreeDeleted {
		t.Error("DryRun with Delete should report WorktreeDeleted = true")
	}

	// Verify files still exist
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Error("DryRun should not delete project files")
	}
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Error("DryRun should not delete worktree files")
	}
}

func TestRemove_DeleteFiles(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create projects directory with a project (not a submodule)
	projectsDir := filepath.Join(tmpDir, "projects")
	projectPath := filepath.Join(projectsDir, "test-project")
	os.MkdirAll(projectPath, 0755)
	os.WriteFile(filepath.Join(projectPath, "file.txt"), []byte("test"), 0644)

	ctx := context.Background()
	result, err := Remove(ctx, tmpDir, "test-project", RemoveOptions{
		Delete: true,
		Force:  true,
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Check result
	if !result.FilesDeleted {
		t.Error("result.FilesDeleted should be true")
	}

	// Verify files are deleted
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Error("Project files should be deleted")
	}
}

func TestRemove_DeleteWithWorktrees(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create project
	projectsDir := filepath.Join(tmpDir, "projects")
	projectPath := filepath.Join(projectsDir, "test-project")
	os.MkdirAll(projectPath, 0755)

	// Create worktrees
	worktreesDir := filepath.Join(tmpDir, "worktrees")
	worktreePath := filepath.Join(worktreesDir, "test-project")
	os.MkdirAll(worktreePath, 0755)
	os.WriteFile(filepath.Join(worktreePath, "branch.txt"), []byte("test"), 0644)

	ctx := context.Background()
	result, err := Remove(ctx, tmpDir, "test-project", RemoveOptions{
		Delete: true,
		Force:  true,
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Check result
	if !result.WorktreeDeleted {
		t.Error("result.WorktreeDeleted should be true")
	}

	// Verify worktrees are deleted
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("Worktree files should be deleted")
	}
}

func TestRemove_NoDeleteKeepsFiles(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create project (not a submodule, so submodule removal will be skipped)
	projectsDir := filepath.Join(tmpDir, "projects")
	projectPath := filepath.Join(projectsDir, "test-project")
	os.MkdirAll(projectPath, 0755)
	os.WriteFile(filepath.Join(projectPath, "file.txt"), []byte("test"), 0644)

	ctx := context.Background()
	result, err := Remove(ctx, tmpDir, "test-project", RemoveOptions{
		Delete: false,
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Check result - no files should be deleted
	if result.FilesDeleted {
		t.Error("result.FilesDeleted should be false when Delete=false")
	}

	// Verify files still exist
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Error("Files should remain when Delete=false")
	}
}

func TestRemove_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Remove(ctx, "/some/path", "test", RemoveOptions{})
	if err != context.Canceled {
		t.Errorf("Remove() error = %v, want %v", err, context.Canceled)
	}
}

func TestRemove_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := Remove(ctx, "/some/path", "test", RemoveOptions{})
	if err != context.DeadlineExceeded {
		t.Errorf("Remove() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestErrProjectNotFound_Error(t *testing.T) {
	err := &ErrProjectNotFound{Name: "test-project"}
	expected := "project not found: test-project"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestIsGitSubmodule_NoGitmodules(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	ctx := context.Background()
	isSubmodule, err := isGitSubmodule(ctx, tmpDir, "test")
	if err != nil {
		t.Fatalf("isGitSubmodule() error = %v", err)
	}

	if isSubmodule {
		t.Error("should return false when no .gitmodules exists")
	}
}

func TestRemove_BoundaryEnforcement(t *testing.T) {
	tmp := t.TempDir()
	tmp, _ = filepath.EvalSymlinks(tmp)

	campaignRoot := filepath.Join(tmp, "campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create an outside directory that the symlink will point to.
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	// Symlink: campaign/projects/escape -> outside (resolves outside root).
	escapeLink := filepath.Join(campaignRoot, "projects", "escape")
	if err := os.Symlink(outside, escapeLink); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	ctx := context.Background()
	_, err := Remove(ctx, campaignRoot, "escape", RemoveOptions{Delete: true})
	if err == nil {
		t.Error("expected boundary error for symlink-escaped project, got nil")
	}
	if !errors.Is(err, pathutil.ErrOutsideBoundary) {
		t.Errorf("expected ErrOutsideBoundary, got: %v", err)
	}
}

func TestRemove_PartialFailureReportsAllErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on Windows")
	}

	tmp := t.TempDir()
	tmp, _ = filepath.EvalSymlinks(tmp)

	campaignRoot := filepath.Join(tmp, "campaign")
	projectDir := filepath.Join(campaignRoot, "projects", "myproj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	worktreeDir := filepath.Join(campaignRoot, "worktrees", "myproj")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Make worktrees parent read-only so RemoveAll on the child fails.
	worktreesParent := filepath.Join(campaignRoot, "worktrees")
	if err := os.Chmod(worktreesParent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(worktreesParent, 0o755) })

	ctx := context.Background()
	result, err := Remove(ctx, campaignRoot, "myproj", RemoveOptions{Delete: true})

	if result == nil || !result.FilesDeleted {
		t.Error("expected FilesDeleted=true even on partial failure")
	}

	if err == nil {
		t.Error("expected error about worktree deletion failure, got nil")
	}

	if result != nil && result.WorktreeDeleted {
		t.Error("expected WorktreeDeleted=false when worktree deletion fails")
	}
}
