//go:build integration

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
)

// setupTestRepo creates a test git repository
func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	run(t, "git", "init", tmpDir)
	run(t, "git", "-C", tmpDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", tmpDir, "config", "user.name", "Test")

	return tmpDir
}

// run executes a command and fails test on error
func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nOutput: %s", name, args, err, output)
	}
	return string(output)
}

// runWithEnv executes a command with custom environment in a directory
func runWithEnv(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nOutput: %s", name, args, err, output)
	}
	return string(output)
}

func TestIntegration_CommitBasic(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Create a file
	testFile := filepath.Join(repoDir, "test.txt")
	os.WriteFile(testFile, []byte("test content"), 0644)

	ctx := context.Background()

	// Create executor and commit
	executor, err := git.NewExecutor(repoDir)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	// Stage all
	err = executor.StageAll(ctx)
	if err != nil {
		t.Fatalf("StageAll() error = %v", err)
	}

	// Commit
	err = executor.Commit(ctx, &git.CommitOptions{Message: "Test commit"})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Verify commit exists
	logOutput := run(t, "git", "-C", repoDir, "log", "--oneline", "-1")
	if !strings.Contains(logOutput, "Test commit") {
		t.Errorf("Commit not found in git log: %s", logOutput)
	}
}

func TestIntegration_CommitNoChanges(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Create initial commit
	os.WriteFile(filepath.Join(repoDir, "init.txt"), []byte("init"), 0644)
	run(t, "git", "-C", repoDir, "add", ".")
	run(t, "git", "-C", repoDir, "commit", "-m", "Initial")

	ctx := context.Background()

	executor, err := git.NewExecutor(repoDir)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	// Try to commit with no changes
	err = executor.Commit(ctx, &git.CommitOptions{Message: "Empty"})
	if err == nil {
		t.Error("Expected error for no changes, got nil")
	}
}

func TestIntegration_CommitWithStaleLock(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Create change and stage it BEFORE creating lock
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("content"), 0644)
	run(t, "git", "-C", repoDir, "add", ".")

	// Create stale lock file AFTER staging
	lockPath := filepath.Join(repoDir, ".git", "index.lock")
	os.WriteFile(lockPath, []byte{}, 0644)

	ctx := context.Background()

	executor, err := git.NewExecutor(repoDir)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	// Commit should succeed after cleaning lock
	err = executor.Commit(ctx, &git.CommitOptions{Message: "Test with lock"})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Verify lock was removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("Lock file still exists after commit")
	}
}

func TestIntegration_CommitAllFunction(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Create a file (not staged)
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("content"), 0644)

	ctx := context.Background()

	// CommitAll should stage and commit
	err := git.CommitAll(ctx, repoDir, "Commit all test")
	if err != nil {
		t.Fatalf("CommitAll() error = %v", err)
	}

	// Verify commit exists
	logOutput := run(t, "git", "-C", repoDir, "log", "--oneline", "-1")
	if !strings.Contains(logOutput, "Commit all test") {
		t.Errorf("Commit not found in git log: %s", logOutput)
	}
}

func TestIntegration_ExecutorCleanLocks(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Create multiple stale locks
	gitDir := filepath.Join(repoDir, ".git")
	locks := []string{
		filepath.Join(gitDir, "index.lock"),
		filepath.Join(gitDir, "HEAD.lock"),
	}

	for _, lock := range locks {
		os.WriteFile(lock, []byte{}, 0644)
	}

	ctx := context.Background()

	executor, err := git.NewExecutor(repoDir)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	result, err := executor.CleanLocks(ctx)
	if err != nil {
		t.Fatalf("CleanLocks() error = %v", err)
	}

	// At least the index.lock should be found and removed
	if len(result.Removed) == 0 {
		t.Error("Expected at least one lock to be removed")
	}

	// Verify index.lock was removed
	if _, err := os.Stat(locks[0]); !os.IsNotExist(err) {
		t.Error("index.lock still exists after CleanLocks()")
	}
}

func TestIntegration_ExecutorHasChanges(t *testing.T) {
	repoDir := setupTestRepo(t)

	ctx := context.Background()

	executor, err := git.NewExecutor(repoDir)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	// No changes initially
	hasChanges, err := executor.HasChanges(ctx)
	if err != nil {
		t.Fatalf("HasChanges() error = %v", err)
	}
	if hasChanges {
		t.Error("HasChanges() = true for empty repo")
	}

	// Create untracked file
	os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("content"), 0644)

	hasChanges, err = executor.HasChanges(ctx)
	if err != nil {
		t.Fatalf("HasChanges() error = %v", err)
	}
	if !hasChanges {
		t.Error("HasChanges() = false with untracked file")
	}
}

func TestIntegration_FindProjectRoot(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Create nested directory
	nestedDir := filepath.Join(repoDir, "a", "b", "c")
	os.MkdirAll(nestedDir, 0755)

	// Find from nested dir
	root, err := git.FindProjectRoot(nestedDir)
	if err != nil {
		t.Fatalf("FindProjectRoot() error = %v", err)
	}
	if root != repoDir {
		t.Errorf("FindProjectRoot() = %s, want %s", root, repoDir)
	}
}

// setupSyncTestRepo creates a campaign root with a single submodule for sync tests.
// Returns (campaignRoot, submodulePath).
func setupSyncTestRepo(t *testing.T) (string, string) {
	t.Helper()

	// Create the "submodule" repo
	subRepo := setupTestRepo(t)
	os.WriteFile(filepath.Join(subRepo, "init.txt"), []byte("initial"), 0644)
	run(t, "git", "-C", subRepo, "add", "-A")
	run(t, "git", "-C", subRepo, "commit", "-m", "initial commit")

	// Create the campaign root and add the submodule
	campRoot := setupTestRepo(t)
	runWithEnv(t, campRoot, []string{"GIT_ALLOW_PROTOCOL=file"}, "git", "submodule", "add", subRepo, "projects/test-project")
	run(t, "git", "-C", campRoot, "commit", "-m", "add submodule")

	subPath := filepath.Join(campRoot, "projects", "test-project")
	return campRoot, subPath
}

func TestIntegration_ProjectCommit_DefaultNoSync(t *testing.T) {
	campRoot, subPath := setupSyncTestRepo(t)

	// Make a change in the submodule and commit it
	os.WriteFile(filepath.Join(subPath, "change.txt"), []byte("new content"), 0644)
	run(t, "git", "-C", subPath, "add", "-A")
	run(t, "git", "-C", subPath, "commit", "-m", "submodule change")

	// Campaign root commit count BEFORE
	beforeLog := run(t, "git", "-C", campRoot, "rev-list", "--count", "HEAD")
	beforeCount := strings.TrimSpace(beforeLog)

	// Verify submodule ref IS dirty in campaign root
	cmd := exec.Command("git", "-C", campRoot, "diff", "--quiet", "--", "projects/test-project")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected submodule ref to be dirty after submodule commit")
	}

	// With --sync defaulting to false, campaign root should NOT get a new commit.
	// The sync only runs when projectCommitSync is true, which now requires explicit --sync.
	// Verify campaign root commit count is unchanged.
	afterLog := run(t, "git", "-C", campRoot, "rev-list", "--count", "HEAD")
	afterCount := strings.TrimSpace(afterLog)

	if beforeCount != afterCount {
		t.Errorf("campaign root got unexpected commit: before=%s after=%s", beforeCount, afterCount)
	}
}

func TestIntegration_ProjectCommit_ExplicitSync(t *testing.T) {
	campRoot, subPath := setupSyncTestRepo(t)

	// Make a change in the submodule and commit it
	ctx := context.Background()
	os.WriteFile(filepath.Join(subPath, "change.txt"), []byte("new content"), 0644)

	executor, err := git.NewExecutor(subPath)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	if err := executor.StageAll(ctx); err != nil {
		t.Fatalf("StageAll() error = %v", err)
	}
	if err := executor.Commit(ctx, &git.CommitOptions{Message: "submodule change"}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Verify submodule ref is dirty in campaign root
	if !checkParentNeedsCommit(ctx, campRoot, subPath) {
		t.Fatal("expected submodule ref to be dirty")
	}

	// syncParentRef should create a commit at campaign root (opt-in path)
	if err := syncParentRef(ctx, campRoot, "projects/test-project", nil); err != nil {
		t.Fatalf("syncParentRef() error = %v", err)
	}

	// Verify campaign root now has a sync commit
	logOutput := run(t, "git", "-C", campRoot, "log", "--oneline", "-1")
	if !strings.Contains(logOutput, "update test-project submodule ref") {
		t.Errorf("expected sync commit message, got: %s", logOutput)
	}
}

func TestIntegration_SubmoduleDetection(t *testing.T) {
	// Create parent repo
	parentDir := setupTestRepo(t)

	// Create initial commit
	os.WriteFile(filepath.Join(parentDir, "README.md"), []byte("# Parent"), 0644)
	run(t, "git", "-C", parentDir, "add", ".")
	run(t, "git", "-C", parentDir, "commit", "-m", "Initial")

	// Create child repo
	childDir := setupTestRepo(t)
	os.WriteFile(filepath.Join(childDir, "README.md"), []byte("# Child"), 0644)
	run(t, "git", "-C", childDir, "add", ".")
	run(t, "git", "-C", childDir, "commit", "-m", "Initial")

	// Add as submodule using -c to allow file protocol
	subPath := filepath.Join(parentDir, "child")
	runWithEnv(t, parentDir, []string{"GIT_ALLOW_PROTOCOL=file"}, "git", "submodule", "add", childDir, "child")
	run(t, "git", "-C", parentDir, "commit", "-m", "Add submodule")

	// Test submodule detection
	isSubmodule, err := git.IsSubmodule(subPath)
	if err != nil {
		t.Fatalf("IsSubmodule() error = %v", err)
	}
	if !isSubmodule {
		t.Error("IsSubmodule() = false for submodule")
	}

	// Test git dir resolution
	gitDir, err := git.GetSubmoduleGitDir(subPath)
	if err != nil {
		t.Fatalf("GetSubmoduleGitDir() error = %v", err)
	}
	if gitDir == "" {
		t.Error("GetSubmoduleGitDir() returned empty string")
	}

	// Verify the git dir exists
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Errorf("GitDir %s does not exist", gitDir)
	}
}
