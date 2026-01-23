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
