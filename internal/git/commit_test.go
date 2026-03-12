package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	cmd := exec.Command("git", "init", tmpDir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	// Configure git for test commits
	exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	return tmpDir
}

func TestStageAll(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	// Stage all
	ctx := context.Background()
	err := StageAll(ctx, tmpDir)
	if err != nil {
		t.Fatalf("StageAll() error = %v", err)
	}

	// Verify staged
	hasStaged, err := HasStagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasStagedChanges() error = %v", err)
	}
	if !hasStaged {
		t.Error("Expected staged changes after StageAll()")
	}
}

func TestStageFiles(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create two files
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)

	ctx := context.Background()
	// Stage only a.txt
	err := StageFiles(ctx, tmpDir, "a.txt")
	if err != nil {
		t.Fatalf("StageFiles() error = %v", err)
	}

	// Check that only a.txt is staged (b.txt should be unstaged)
	cmd := exec.Command("git", "-C", tmpDir, "diff", "--cached", "--name-only")
	output, _ := cmd.Output()
	staged := strings.TrimSpace(string(output))

	if staged != "a.txt" {
		t.Errorf("Staged files = %v, want a.txt", staged)
	}
}

func TestStageFiles_NoFiles(t *testing.T) {
	tmpDir := initTestRepo(t)

	ctx := context.Background()
	err := StageFiles(ctx, tmpDir)
	if err == nil {
		t.Error("StageFiles() with no files should return error")
	}
}

func TestStage_InvalidPath(t *testing.T) {
	tmpDir := initTestRepo(t)

	ctx := context.Background()
	err := StageFiles(ctx, tmpDir, "nonexistent-file.txt")
	if err == nil {
		t.Error("Stage() with invalid path should return error")
	}
}

func TestHasStagedChanges_NoChanges(t *testing.T) {
	tmpDir := initTestRepo(t)

	ctx := context.Background()
	hasStaged, err := HasStagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasStagedChanges() error = %v", err)
	}
	if hasStaged {
		t.Error("Expected no staged changes in empty repo")
	}
}

func TestHasStagedChanges_WithChanges(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create and stage a file
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	exec.Command("git", "-C", tmpDir, "add", ".").Run()

	ctx := context.Background()
	hasStaged, err := HasStagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasStagedChanges() error = %v", err)
	}
	if !hasStaged {
		t.Error("Expected staged changes")
	}
}

func TestHasUnstagedChanges_NoChanges(t *testing.T) {
	tmpDir := initTestRepo(t)

	ctx := context.Background()
	hasUnstaged, err := HasUnstagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasUnstagedChanges() error = %v", err)
	}
	if hasUnstaged {
		t.Error("Expected no unstaged changes in empty repo")
	}
}

func TestHasUnstagedChanges_WithChanges(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create initial commit
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	exec.Command("git", "-C", tmpDir, "add", ".").Run()
	exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

	// Modify the file without staging
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("modified"), 0644)

	ctx := context.Background()
	hasUnstaged, err := HasUnstagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasUnstagedChanges() error = %v", err)
	}
	if !hasUnstaged {
		t.Error("Expected unstaged changes")
	}
}

func TestHasUntrackedFiles_NoFiles(t *testing.T) {
	tmpDir := initTestRepo(t)

	ctx := context.Background()
	hasUntracked, err := HasUntrackedFiles(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasUntrackedFiles() error = %v", err)
	}
	if hasUntracked {
		t.Error("Expected no untracked files in empty repo")
	}
}

func TestHasUntrackedFiles_WithFiles(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create untracked file
	os.WriteFile(filepath.Join(tmpDir, "untracked.txt"), []byte("content"), 0644)

	ctx := context.Background()
	hasUntracked, err := HasUntrackedFiles(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasUntrackedFiles() error = %v", err)
	}
	if !hasUntracked {
		t.Error("Expected untracked files")
	}
}

func TestHasChanges(t *testing.T) {
	t.Run("no changes", func(t *testing.T) {
		tmpDir := initTestRepo(t)

		ctx := context.Background()
		hasChanges, err := HasChanges(ctx, tmpDir)
		if err != nil {
			t.Fatalf("HasChanges() error = %v", err)
		}
		if hasChanges {
			t.Error("Expected no changes in empty repo")
		}
	})

	t.Run("with untracked", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		os.WriteFile(filepath.Join(tmpDir, "new.txt"), []byte("content"), 0644)

		ctx := context.Background()
		hasChanges, err := HasChanges(ctx, tmpDir)
		if err != nil {
			t.Fatalf("HasChanges() error = %v", err)
		}
		if !hasChanges {
			t.Error("Expected changes with untracked file")
		}
	})

	t.Run("with staged", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		os.WriteFile(filepath.Join(tmpDir, "new.txt"), []byte("content"), 0644)
		exec.Command("git", "-C", tmpDir, "add", ".").Run()

		ctx := context.Background()
		hasChanges, err := HasChanges(ctx, tmpDir)
		if err != nil {
			t.Fatalf("HasChanges() error = %v", err)
		}
		if !hasChanges {
			t.Error("Expected changes with staged file")
		}
	})
}

func TestStage_ContextCancellation(t *testing.T) {
	tmpDir := initTestRepo(t)
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := StageAll(ctx, tmpDir)
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestCommitOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		opts    CommitOptions
		wantErr bool
	}{
		{
			name:    "valid with message",
			opts:    CommitOptions{Message: "test commit"},
			wantErr: false,
		},
		{
			name:    "valid amend without message",
			opts:    CommitOptions{Amend: true},
			wantErr: false,
		},
		{
			name:    "invalid empty message no amend",
			opts:    CommitOptions{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCommit_NilOptions(t *testing.T) {
	tmpDir := initTestRepo(t)
	ctx := context.Background()

	err := Commit(ctx, tmpDir, nil)
	if err == nil {
		t.Error("Commit() with nil options should return error")
	}
}

func TestCommit_InvalidOptions(t *testing.T) {
	tmpDir := initTestRepo(t)
	ctx := context.Background()

	err := Commit(ctx, tmpDir, &CommitOptions{})
	if err == nil {
		t.Error("Commit() with empty message should return error")
	}
}

func TestCommit_Success(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create and stage a file
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	StageAll(context.Background(), tmpDir)

	// Commit
	ctx := context.Background()
	err := Commit(ctx, tmpDir, &CommitOptions{Message: "test commit"})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Verify commit exists
	cmd := exec.Command("git", "-C", tmpDir, "log", "--oneline", "-1")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), "test commit") {
		t.Error("Commit message not found in git log")
	}
}

func TestCommit_NoChanges(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create initial commit so we have a HEAD
	os.WriteFile(filepath.Join(tmpDir, "init.txt"), []byte("init"), 0644)
	exec.Command("git", "-C", tmpDir, "add", ".").Run()
	exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

	ctx := context.Background()
	err := Commit(ctx, tmpDir, &CommitOptions{Message: "empty"})

	if err == nil {
		t.Error("Commit() should return error for no changes")
	}
	// Note: ErrNoChanges is returned
}

func TestCommit_WithAmend(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create initial commit
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	exec.Command("git", "-C", tmpDir, "add", ".").Run()
	exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run()

	// Amend with new message
	ctx := context.Background()
	err := Commit(ctx, tmpDir, &CommitOptions{Message: "amended commit", Amend: true})
	if err != nil {
		t.Fatalf("Commit() with amend error = %v", err)
	}

	// Verify amended message
	cmd := exec.Command("git", "-C", tmpDir, "log", "--oneline", "-1")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), "amended commit") {
		t.Error("Amended message not found in git log")
	}
}

func TestCommit_WithAllowEmpty(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create initial commit
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	exec.Command("git", "-C", tmpDir, "add", ".").Run()
	exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run()

	// Empty commit
	ctx := context.Background()
	err := Commit(ctx, tmpDir, &CommitOptions{Message: "empty commit", AllowEmpty: true})
	if err != nil {
		t.Fatalf("Commit() with allow-empty error = %v", err)
	}

	// Verify commit exists
	cmd := exec.Command("git", "-C", tmpDir, "log", "--oneline", "-1")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), "empty commit") {
		t.Error("Empty commit message not found in git log")
	}
}

func TestCommit_WithAuthor(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create and stage a file
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	StageAll(context.Background(), tmpDir)

	// Commit with custom author
	ctx := context.Background()
	err := Commit(ctx, tmpDir, &CommitOptions{
		Message: "custom author commit",
		Author:  "Custom Author <custom@example.com>",
	})
	if err != nil {
		t.Fatalf("Commit() with author error = %v", err)
	}

	// Verify author
	cmd := exec.Command("git", "-C", tmpDir, "log", "--format=%an <%ae>", "-1")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), "Custom Author") {
		t.Errorf("Custom author not found: %s", string(output))
	}
}

func TestCommit_WithStaleLock(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create change
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	StageAll(context.Background(), tmpDir)

	// Create stale lock
	lockPath := filepath.Join(tmpDir, ".git", "index.lock")
	os.WriteFile(lockPath, []byte{}, 0644)

	// Commit should succeed after cleaning lock
	ctx := context.Background()
	err := Commit(ctx, tmpDir, &CommitOptions{Message: "test"})
	if err != nil {
		t.Fatalf("Commit() error = %v (should have cleaned stale lock)", err)
	}

	// Verify lock was removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("Stale lock still exists after commit")
	}
}

func TestCommitAll_Success(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create file (not staged)
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	// CommitAll should stage and commit
	ctx := context.Background()
	err := CommitAll(ctx, tmpDir, "commit all test")
	if err != nil {
		t.Fatalf("CommitAll() error = %v", err)
	}

	// Verify commit exists
	cmd := exec.Command("git", "-C", tmpDir, "log", "--oneline", "-1")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), "commit all test") {
		t.Error("CommitAll message not found in git log")
	}
}

func TestCommitAll_NoChanges(t *testing.T) {
	tmpDir := initTestRepo(t)

	ctx := context.Background()
	err := CommitAll(ctx, tmpDir, "empty")

	if err == nil {
		t.Error("CommitAll() should return error for no changes")
	}
}

func TestIsLockError(t *testing.T) {
	t.Run("returns true for LockError", func(t *testing.T) {
		err := &LockError{Path: "/some/path"}
		if !isLockError(err) {
			t.Error("isLockError() = false for LockError, want true")
		}
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		err := errors.New("some other error")
		if isLockError(err) {
			t.Error("isLockError() = true for non-LockError, want false")
		}
	})

	t.Run("returns true for wrapped LockError", func(t *testing.T) {
		lockErr := &LockError{Path: "/some/path"}
		wrapped := fmt.Errorf("wrapped: %w", lockErr)
		if !isLockError(wrapped) {
			t.Error("isLockError() = false for wrapped LockError, want true")
		}
	})
}

func TestStageAllExcluding_ExcludesPaths(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create files in different directories
	os.MkdirAll(filepath.Join(tmpDir, "projects", "camp"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "festivals"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "festivals", "plan.md"), []byte("plan"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "projects", "camp", "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("readme"), 0644)

	ctx := context.Background()
	err := StageAllExcluding(ctx, tmpDir, []string{"projects/camp"})
	if err != nil {
		t.Fatalf("StageAllExcluding() error = %v", err)
	}

	// Check what's staged
	cmd := exec.Command("git", "-C", tmpDir, "diff", "--cached", "--name-only")
	output, _ := cmd.Output()
	staged := strings.TrimSpace(string(output))

	if !strings.Contains(staged, "festivals/plan.md") {
		t.Error("Expected festivals/plan.md to be staged")
	}
	if !strings.Contains(staged, "README.md") {
		t.Error("Expected README.md to be staged")
	}
	if strings.Contains(staged, "projects/camp") {
		t.Error("Expected projects/camp to be excluded from staging")
	}
}

func TestStageAllExcluding_NoExclusions(t *testing.T) {
	tmpDir := initTestRepo(t)

	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	ctx := context.Background()
	err := StageAllExcluding(ctx, tmpDir, nil)
	if err != nil {
		t.Fatalf("StageAllExcluding() with nil exclusions error = %v", err)
	}

	hasStaged, err := HasStagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasStagedChanges() error = %v", err)
	}
	if !hasStaged {
		t.Error("Expected staged changes when no exclusions provided")
	}
}

func TestStageAllExcluding_EmptyExclusions(t *testing.T) {
	tmpDir := initTestRepo(t)

	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	ctx := context.Background()
	err := StageAllExcluding(ctx, tmpDir, []string{})
	if err != nil {
		t.Fatalf("StageAllExcluding() with empty exclusions error = %v", err)
	}

	hasStaged, err := HasStagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasStagedChanges() error = %v", err)
	}
	if !hasStaged {
		t.Error("Expected staged changes when empty exclusions provided")
	}
}

func TestStageAllExcluding_CancelledContext(t *testing.T) {
	tmpDir := initTestRepo(t)
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := StageAllExcluding(ctx, tmpDir, []string{"some/path"})
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestStageAllExcluding_MultipleExclusions(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create files in multiple directories
	os.MkdirAll(filepath.Join(tmpDir, "projects", "alpha"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "projects", "beta"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "docs"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "projects", "alpha", "go.mod"), []byte("module alpha"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "projects", "beta", "go.mod"), []byte("module beta"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "docs", "guide.md"), []byte("guide"), 0644)

	ctx := context.Background()
	err := StageAllExcluding(ctx, tmpDir, []string{"projects/alpha", "projects/beta"})
	if err != nil {
		t.Fatalf("StageAllExcluding() error = %v", err)
	}

	cmd := exec.Command("git", "-C", tmpDir, "diff", "--cached", "--name-only")
	output, _ := cmd.Output()
	staged := strings.TrimSpace(string(output))

	if !strings.Contains(staged, "docs/guide.md") {
		t.Error("Expected docs/guide.md to be staged")
	}
	if strings.Contains(staged, "projects/alpha") {
		t.Error("Expected projects/alpha to be excluded")
	}
	if strings.Contains(staged, "projects/beta") {
		t.Error("Expected projects/beta to be excluded")
	}
}

func TestFilterTracked(t *testing.T) {
	t.Run("empty paths", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		ctx := context.Background()
		result, err := FilterTracked(ctx, tmpDir, nil)
		if err != nil {
			t.Fatalf("FilterTracked() error = %v", err)
		}
		if result != nil {
			t.Errorf("FilterTracked() = %v, want nil", result)
		}
	})

	t.Run("tracked file returned", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
		exec.Command("git", "-C", tmpDir, "add", "a.txt").Run()
		exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

		ctx := context.Background()
		result, err := FilterTracked(ctx, tmpDir, []string{"a.txt"})
		if err != nil {
			t.Fatalf("FilterTracked() error = %v", err)
		}
		if len(result) != 1 || result[0] != "a.txt" {
			t.Errorf("FilterTracked() = %v, want [a.txt]", result)
		}
	})

	t.Run("untracked file excluded", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
		exec.Command("git", "-C", tmpDir, "add", "a.txt").Run()
		exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()
		os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)

		ctx := context.Background()
		result, err := FilterTracked(ctx, tmpDir, []string{"a.txt", "b.txt"})
		if err != nil {
			t.Fatalf("FilterTracked() error = %v", err)
		}
		if len(result) != 1 || result[0] != "a.txt" {
			t.Errorf("FilterTracked() = %v, want [a.txt]", result)
		}
	})

	t.Run("nonexistent path excluded", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
		exec.Command("git", "-C", tmpDir, "add", "a.txt").Run()
		exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

		ctx := context.Background()
		result, err := FilterTracked(ctx, tmpDir, []string{"a.txt", "nonexistent.txt"})
		if err != nil {
			t.Fatalf("FilterTracked() error = %v", err)
		}
		if len(result) != 1 || result[0] != "a.txt" {
			t.Errorf("FilterTracked() = %v, want [a.txt]", result)
		}
	})

	t.Run("directory with tracked files", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
		os.WriteFile(filepath.Join(tmpDir, "subdir", "file.txt"), []byte("content"), 0644)
		exec.Command("git", "-C", tmpDir, "add", ".").Run()
		exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

		ctx := context.Background()
		result, err := FilterTracked(ctx, tmpDir, []string{"subdir"})
		if err != nil {
			t.Fatalf("FilterTracked() error = %v", err)
		}
		if len(result) != 1 || result[0] != "subdir" {
			t.Errorf("FilterTracked() = %v, want [subdir]", result)
		}
	})

	t.Run("renamed directory not tracked", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		os.MkdirAll(filepath.Join(tmpDir, "old-name"), 0755)
		os.WriteFile(filepath.Join(tmpDir, "old-name", "file.txt"), []byte("content"), 0644)
		exec.Command("git", "-C", tmpDir, "add", ".").Run()
		exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

		// Rename without telling git
		os.Rename(filepath.Join(tmpDir, "old-name"), filepath.Join(tmpDir, "new-name"))

		ctx := context.Background()
		result, err := FilterTracked(ctx, tmpDir, []string{"new-name"})
		if err != nil {
			t.Fatalf("FilterTracked() error = %v", err)
		}
		if len(result) != 0 {
			t.Errorf("FilterTracked() = %v, want empty (new-name was never tracked)", result)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := FilterTracked(ctx, tmpDir, []string{"anything"})
		if err == nil {
			t.Error("Expected error for cancelled context")
		}
	})
}

func TestExpandTrackedPaths(t *testing.T) {
	t.Run("staged directory expands to tracked descendants", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		if err := os.MkdirAll(filepath.Join(tmpDir, "docs", "T1", "test3"), 0755); err != nil {
			t.Fatalf("failed to create nested dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "docs", "T1", "test3", "note.md"), []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create nested file: %v", err)
		}

		ctx := context.Background()
		if err := StageFiles(ctx, tmpDir, "docs/T1/test3"); err != nil {
			t.Fatalf("StageFiles() error = %v", err)
		}

		result, err := ExpandTrackedPaths(ctx, tmpDir, []string{"docs/T1/test3"})
		if err != nil {
			t.Fatalf("ExpandTrackedPaths() error = %v", err)
		}
		if len(result) != 1 || result[0] != "docs/T1/test3/note.md" {
			t.Fatalf("ExpandTrackedPaths() = %v, want [docs/T1/test3/note.md]", result)
		}
	})

	t.Run("staged deleted directory expands to deleted descendants", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		if err := os.MkdirAll(filepath.Join(tmpDir, "old-name"), 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "old-name", "file.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		exec.Command("git", "-C", tmpDir, "add", ".").Run()
		exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()
		if err := os.RemoveAll(filepath.Join(tmpDir, "old-name")); err != nil {
			t.Fatalf("failed to remove dir: %v", err)
		}

		ctx := context.Background()
		if err := StageTrackedChanges(ctx, tmpDir, "old-name"); err != nil {
			t.Fatalf("StageTrackedChanges() error = %v", err)
		}

		result, err := ExpandTrackedPaths(ctx, tmpDir, []string{"old-name"})
		if err != nil {
			t.Fatalf("ExpandTrackedPaths() error = %v", err)
		}
		if len(result) != 1 || result[0] != "old-name/file.txt" {
			t.Fatalf("ExpandTrackedPaths() = %v, want [old-name/file.txt]", result)
		}
	})

	t.Run("staged rename returns source and destination paths", func(t *testing.T) {
		tmpDir := initTestRepo(t)
		if err := os.MkdirAll(filepath.Join(tmpDir, "dungeon", "archived"), 0755); err != nil {
			t.Fatalf("failed to create archived dir: %v", err)
		}
		sourcePath := filepath.Join(tmpDir, "stale-doc.md")
		if err := os.WriteFile(sourcePath, []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}
		exec.Command("git", "-C", tmpDir, "add", ".").Run()
		exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

		destPath := filepath.Join(tmpDir, "dungeon", "archived", "stale-doc.md")
		if err := os.Rename(sourcePath, destPath); err != nil {
			t.Fatalf("failed to rename source file: %v", err)
		}

		ctx := context.Background()
		if err := StageFiles(ctx, tmpDir, "stale-doc.md", "dungeon/archived/stale-doc.md"); err != nil {
			t.Fatalf("StageFiles() error = %v", err)
		}

		result, err := ExpandTrackedPaths(ctx, tmpDir, []string{"stale-doc.md", "dungeon/archived/stale-doc.md"})
		if err != nil {
			t.Fatalf("ExpandTrackedPaths() error = %v", err)
		}
		if len(result) != 2 || result[0] != "stale-doc.md" || result[1] != "dungeon/archived/stale-doc.md" {
			t.Fatalf("ExpandTrackedPaths() = %v, want [stale-doc.md dungeon/archived/stale-doc.md]", result)
		}
	})
}

func TestStage_WithStaleLock(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create a file to stage
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	// Create stale lock file
	lockPath := filepath.Join(tmpDir, ".git", "index.lock")
	os.WriteFile(lockPath, []byte{}, 0644)

	// Stage should succeed after cleaning lock
	ctx := context.Background()
	err := StageAll(ctx, tmpDir)
	if err != nil {
		t.Fatalf("StageAll() error = %v (should have cleaned stale lock)", err)
	}

	// Verify lock was removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("Stale lock still exists after stage")
	}

	// Verify file was staged
	hasStaged, err := HasStagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasStagedChanges() error = %v", err)
	}
	if !hasStaged {
		t.Error("File was not staged after lock cleanup")
	}
}

func TestStage_WaitsForBriefActiveLock(t *testing.T) {
	tmpDir := initTestRepo(t)

	// Create a file to stage.
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(tmpDir, ".git", "index.lock")
	f, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = f.Close()
		_ = os.Remove(lockPath)
	})

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = f.Close()
		_ = os.Remove(lockPath)
	}()

	ctx := context.Background()
	if err := StageAll(ctx, tmpDir); err != nil {
		t.Fatalf("StageAll() error = %v (should have waited for active lock release)", err)
	}

	hasStaged, err := HasStagedChanges(ctx, tmpDir)
	if err != nil {
		t.Fatalf("HasStagedChanges() error = %v", err)
	}
	if !hasStaged {
		t.Error("File was not staged after active lock release")
	}
}

func TestStage_ReturnsRemovalFailureForStaleLock(t *testing.T) {
	tmpDir := initTestRepo(t)

	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(tmpDir, ".git", "index.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	gitDir := filepath.Join(tmpDir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(gitDir, info.Mode().Perm())
		_ = os.Remove(lockPath)
	}()

	if err := os.Chmod(gitDir, 0555); err != nil {
		t.Fatalf("failed to make .git read-only: %v", err)
	}

	err = StageAll(context.Background(), tmpDir)
	if err == nil {
		t.Fatal("StageAll() error = nil, want stale lock removal failure")
	}
	if !errors.Is(err, ErrLockRemovalFailed) {
		t.Fatalf("StageAll() error = %v, want ErrLockRemovalFailed", err)
	}
	if errors.Is(err, ErrLockActive) {
		t.Fatalf("StageAll() error = %v, did not want ErrLockActive", err)
	}
}
