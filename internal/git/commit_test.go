package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func ageLockFileForTest(t *testing.T, lockPath string) {
	t.Helper()

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("age lock %s: %v", lockPath, err)
	}
}

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

func runGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), env...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, strings.TrimSpace(string(output)))
	}

	return strings.TrimSpace(string(output))
}

func setupRepoWithNestedSubmodule(t *testing.T) (string, string) {
	t.Helper()

	nestedBare := t.TempDir()
	runGit(t, "", nil, "init", "--bare", nestedBare)

	nestedSeed := t.TempDir()
	runGit(t, "", nil, "clone", nestedBare, nestedSeed)
	runGit(t, nestedSeed, nil, "config", "user.email", "test@test.com")
	runGit(t, nestedSeed, nil, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(nestedSeed, "README.md"), []byte("nested\n"), 0o644); err != nil {
		t.Fatalf("write nested README: %v", err)
	}
	runGit(t, nestedSeed, nil, "add", ".")
	runGit(t, nestedSeed, nil, "commit", "-m", "init nested")
	runGit(t, nestedSeed, nil, "push", "origin", "main")

	parent := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(parent, "README.md"), []byte("parent\n"), 0o644); err != nil {
		t.Fatalf("write parent README: %v", err)
	}
	runGit(t, parent, nil, "add", ".")
	runGit(t, parent, nil, "commit", "-m", "init parent")
	runGit(t, parent, []string{"GIT_ALLOW_PROTOCOL=file"}, "submodule", "add", nestedBare, "vendor/tool")
	runGit(t, parent, nil, "commit", "-m", "add nested submodule")

	submoduleDir := filepath.Join(parent, "vendor", "tool")
	runGit(t, submoduleDir, nil, "config", "user.email", "test@test.com")
	runGit(t, submoduleDir, nil, "config", "user.name", "Test")

	return parent, submoduleDir
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

func TestHasNonSubmoduleChanges(t *testing.T) {
	t.Run("ignores nested submodule drift", func(t *testing.T) {
		parent, submoduleDir := setupRepoWithNestedSubmodule(t)

		if err := os.WriteFile(filepath.Join(submoduleDir, "nested.txt"), []byte("nested change\n"), 0o644); err != nil {
			t.Fatalf("write nested change: %v", err)
		}
		runGit(t, submoduleDir, nil, "add", ".")
		runGit(t, submoduleDir, nil, "commit", "-m", "nested drift")

		ctx := context.Background()
		hasChanges, err := HasNonSubmoduleChanges(ctx, parent)
		if err != nil {
			t.Fatalf("HasNonSubmoduleChanges() error = %v", err)
		}
		if hasChanges {
			t.Fatal("expected nested submodule drift to be ignored")
		}
	})

	t.Run("reports parent repo changes", func(t *testing.T) {
		parent, _ := setupRepoWithNestedSubmodule(t)

		if err := os.WriteFile(filepath.Join(parent, "README.md"), []byte("parent change\n"), 0o644); err != nil {
			t.Fatalf("write parent change: %v", err)
		}

		ctx := context.Background()
		hasChanges, err := HasNonSubmoduleChanges(ctx, parent)
		if err != nil {
			t.Fatalf("HasNonSubmoduleChanges() error = %v", err)
		}
		if !hasChanges {
			t.Fatal("expected parent repo changes to be reported")
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

func TestCommitArgs_AmendNoEdit(t *testing.T) {
	args := commitArgs("/repo", &CommitOptions{Amend: true, NoEdit: true})
	got := strings.Join(args, " ")
	want := "-C /repo commit --amend --no-edit"
	if got != want {
		t.Fatalf("commitArgs() = %q, want %q", got, want)
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

func TestCommit_WithAmendNoEdit(t *testing.T) {
	tmpDir := initTestRepo(t)

	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, tmpDir, nil, "add", ".")
	runGit(t, tmpDir, nil, "commit", "-m", "initial")

	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := StageAll(context.Background(), tmpDir); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	err := Commit(ctx, tmpDir, &CommitOptions{Amend: true, NoEdit: true})
	if err != nil {
		t.Fatalf("Commit() with amend --no-edit error = %v", err)
	}

	cmd := exec.Command("git", "-C", tmpDir, "log", "--format=%s", "-1")
	output, _ := cmd.Output()
	if strings.TrimSpace(string(output)) != "initial" {
		t.Fatalf("amended commit subject = %q, want initial", strings.TrimSpace(string(output)))
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
	ageLockFileForTest(t, lockPath)

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

func TestFilterTrackedNonASCII(t *testing.T) {
	got := filterTrackedPaths([]string{"docs/cafe.md", "docs/café.md"}, []string{"docs/café.md"})
	if len(got) != 1 || got[0] != "docs/café.md" {
		t.Fatalf("filterTrackedPaths() = %v, want [docs/café.md]", got)
	}
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
	ageLockFileForTest(t, lockPath)

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

	ready := make(chan struct{})
	go func() {
		<-ready
		_ = f.Close()
		_ = os.Remove(lockPath)
	}()
	close(ready)

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
	ageLockFileForTest(t, lockPath)

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

func TestSplitPathspecsForExecKeepsBatchesBelowArgLimit(t *testing.T) {
	const limit = 32 * 1024
	paths := make([]string, 100)
	for i := range paths {
		paths[i] = strings.Repeat("p", 1000)
	}

	batches := splitPathspecsForExec(paths, limit)
	if len(batches) < 2 {
		t.Fatalf("splitPathspecsForExec() returned %d batch(es), want multiple", len(batches))
	}

	var flattened []string
	for i, batch := range batches {
		bytes := 0
		for _, path := range batch {
			bytes += len(path) + 1
		}
		if bytes > limit {
			t.Errorf("batch %d is %d bytes, want at most %d", i, bytes, limit)
		}
		flattened = append(flattened, batch...)
	}

	if len(flattened) != len(paths) {
		t.Fatalf("splitPathspecsForExec() returned %d paths, want %d", len(flattened), len(paths))
	}
	for i := range paths {
		if flattened[i] != paths[i] {
			t.Fatalf("path %d = %q, want %q", i, flattened[i], paths[i])
		}
	}
}

func TestSplitPathspecsForExecEmpty(t *testing.T) {
	if got := splitPathspecsForExec(nil, 1024); len(got) != 0 {
		t.Fatalf("nil paths = %v, want empty", got)
	}
	if got := splitPathspecsForExec([]string{}, 1024); len(got) != 0 {
		t.Fatalf("empty paths = %v, want empty", got)
	}
}

func TestResetPathspecPayloadLimitReservesFixedArgv(t *testing.T) {
	shortRepo := "/r"
	longRepo := "/" + strings.Repeat("x", 2000)
	shortLimit := resetPathspecPayloadLimit(shortRepo)
	longLimit := resetPathspecPayloadLimit(longRepo)
	if longLimit >= shortLimit {
		t.Fatalf("longer repoPath should reduce payload budget: short=%d long=%d", shortLimit, longLimit)
	}
	// Fixed argv for long repo must still leave a usable path budget.
	if longLimit < minResetPathspecPayload {
		t.Fatalf("payload limit %d below floor %d", longLimit, minResetPathspecPayload)
	}
	// Unix budget stays large so multi-MB path lists need few batches.
	if runtime.GOOS != "windows" {
		if platformCommandLineBudget() < 64*1024 {
			t.Fatalf("unix command-line budget %d is too small for performance", platformCommandLineBudget())
		}
		if shortLimit < 64*1024 {
			t.Fatalf("unix path payload for short repo %d, want generous batches", shortLimit)
		}
	}
	// Windows leaves substantial room below the CreateProcess ceiling for
	// os/exec command-line quoting and escaping.
	if runtime.GOOS == "windows" && platformCommandLineBudget() != 16*1024 {
		t.Fatalf("windows budget = %d, want %d", platformCommandLineBudget(), 16*1024)
	}
}

// `camp init` creates the .git directory but never commits, so a job enqueued
// before the user's first commit runs against an unborn branch: HEAD is a ref
// that does not resolve to any object. The tests below cover that precondition
// for every entry point that seeds a temp index from HEAD.

func TestHeadResolvable(t *testing.T) {
	tmpDir := initTestRepo(t)
	ctx := context.Background()

	if headResolvable(ctx, tmpDir) {
		t.Fatal("headResolvable() = true on a freshly initialized repo with no commits")
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "seed.txt"), []byte("seed"), 0644); err != nil {
		t.Fatalf("write seed.txt: %v", err)
	}
	if err := StageAll(ctx, tmpDir); err != nil {
		t.Fatalf("StageAll() error = %v", err)
	}
	if err := Commit(ctx, tmpDir, &CommitOptions{Message: "seed commit"}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if !headResolvable(ctx, tmpDir) {
		t.Fatal("headResolvable() = false once a commit exists")
	}
}

// The bug this guards: `git read-tree HEAD` fails with "Not a valid object
// name HEAD" on an unborn branch, which used to fail every deferred
// bookkeeping job enqueued before the user's first commit.
func TestReadTreeIntoTempIndex_UnbornHead(t *testing.T) {
	tmpDir := initTestRepo(t)
	ctx := context.Background()

	tmpPath, _, err := BuildTempIndexPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildTempIndexPath() error = %v", err)
	}
	defer RemoveTempIndex(tmpPath)

	if err := ReadTreeIntoTempIndex(ctx, tmpDir, tmpPath); err != nil {
		t.Fatalf("ReadTreeIntoTempIndex() on an unborn branch error = %v", err)
	}

	// The seeded index must be usable: staging a path into it and writing a
	// tree should succeed exactly as it would from a HEAD-seeded index.
	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)
	if err := RunWithEnv(ctx, tmpDir, env,
		"update-index", "--add", "--cacheinfo",
		"100644,"+emptyBlobSHA(t, tmpDir)+",seeded.txt"); err != nil {
		t.Fatalf("stage into the empty-seeded temp index: %v", err)
	}
	out := runGit(t, "", env, "-C", tmpDir, "ls-files")
	if !strings.Contains(out, "seeded.txt") {
		t.Fatalf("temp index seeded from an unborn branch did not accept a staged path; ls-files: %q", out)
	}
}

// emptyBlobSHA hashes the empty blob into the object store and returns its
// SHA, so a test can stage a path without needing real file content on disk.
func emptyBlobSHA(t *testing.T, repoPath string) string {
	t.Helper()
	return runGit(t, "", nil, "-C", repoPath, "hash-object", "-w", "--", os.DevNull)
}

// CommitScoped is the path a hand-written (path-only, no captured blobs) job
// takes. It must produce the repository's first commit rather than failing on
// the temp-index seed.
func TestCommitScoped_UnbornHead(t *testing.T) {
	tmpDir := initTestRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(tmpDir, "note.md"), []byte("first note\n"), 0644); err != nil {
		t.Fatalf("write note.md: %v", err)
	}

	if err := CommitScoped(ctx, tmpDir, []string{"note.md"}, &CommitOptions{
		Message: "capture: first note",
	}); err != nil {
		t.Fatalf("CommitScoped() on an unborn branch error = %v", err)
	}

	if !headResolvable(ctx, tmpDir) {
		t.Fatal("CommitScoped() did not create the repository's first commit")
	}
	subject := runGit(t, "", nil, "-C", tmpDir, "log", "-1", "--format=%s")
	if subject != "capture: first note" {
		t.Fatalf("commit subject = %q, want %q", subject, "capture: first note")
	}
	tracked := runGit(t, "", nil, "-C", tmpDir, "ls-tree", "-r", "--name-only", "HEAD")
	if !strings.Contains(tracked, "note.md") {
		t.Fatalf("first commit does not contain note.md; tree: %q", tracked)
	}
	count := runGit(t, "", nil, "-C", tmpDir, "rev-list", "--count", "HEAD")
	if count != "1" {
		t.Fatalf("rev-list --count HEAD = %q, want 1 (exactly one commit)", count)
	}
}

// CommitBlobs is the path a bookkeeping job with captured content takes (the
// worker's real production path for something like `camp intent add`). Content
// is captured up front and committed without reading the working tree or the
// real index, so this exercises the exact mechanism the bug report describes.
func TestCommitBlobs_UnbornHead(t *testing.T) {
	tmpDir := initTestRepo(t)
	ctx := context.Background()

	path := filepath.Join(tmpDir, "intents", "idea.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("an idea captured before the first commit\n"), 0644); err != nil {
		t.Fatalf("write idea.md: %v", err)
	}

	refs, err := CaptureBlobs(ctx, tmpDir, []string{"intents/idea.md"})
	if err != nil {
		t.Fatalf("CaptureBlobs() error = %v", err)
	}

	if err := CommitBlobs(ctx, tmpDir, refs, &CommitOptions{
		Message: "capture intent: an idea",
	}); err != nil {
		t.Fatalf("CommitBlobs() on an unborn branch error = %v", err)
	}

	if !headResolvable(ctx, tmpDir) {
		t.Fatal("CommitBlobs() did not create the repository's first commit")
	}
	tracked := runGit(t, "", nil, "-C", tmpDir, "ls-tree", "-r", "--name-only", "HEAD")
	if !strings.Contains(tracked, "intents/idea.md") {
		t.Fatalf("first commit does not contain the captured blob; tree: %q", tracked)
	}

	// The real index must stay untouched: CommitBlobs commits captured content
	// through a temp index and must never stage the user's working tree.
	status := runGit(t, "", nil, "-C", tmpDir, "status", "--porcelain")
	if status != "" {
		t.Fatalf("CommitBlobs left the real index/working tree dirty: %q", status)
	}
}
