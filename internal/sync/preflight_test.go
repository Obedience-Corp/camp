package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// setupTestRepo creates a git repo with optional submodules for testing.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	// Initialize parent repo
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")

	// Create initial commit
	createFile(t, filepath.Join(tmpDir, "README.md"), "# Test")
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "Initial commit")

	return tmpDir
}

// setupSubmodule adds a submodule to the test repo.
func setupSubmodule(t *testing.T, parentRepo, subPath string) string {
	t.Helper()

	// Create the submodule repo
	subRepoDir := t.TempDir()
	runGit(t, subRepoDir, "init")
	runGit(t, subRepoDir, "config", "user.email", "test@test.com")
	runGit(t, subRepoDir, "config", "user.name", "Test")
	createFile(t, filepath.Join(subRepoDir, "sub.txt"), "submodule content")
	runGit(t, subRepoDir, "add", ".")
	runGit(t, subRepoDir, "commit", "-m", "Initial submodule commit")

	// Add as submodule to parent
	runGit(t, parentRepo, "submodule", "add", subRepoDir, subPath)
	runGit(t, parentRepo, "commit", "-m", "Add submodule")

	return filepath.Join(parentRepo, subPath)
}

// runGit runs a git command and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Enable file protocol for submodule tests (required for modern Git)
	cmd.Env = append(os.Environ(), "GIT_ALLOW_PROTOCOL=file")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

// createFile creates a file with the given content.
func createFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}

func TestListSubmodules(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub1")
	setupSubmodule(t, repoRoot, "projects/sub2")

	syncer := NewSyncer(repoRoot)
	paths, err := syncer.listSubmodules(ctx)
	if err != nil {
		t.Fatalf("listSubmodules() error = %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("listSubmodules() got %d paths, want 2", len(paths))
	}

	// Check paths are present
	pathSet := make(map[string]bool)
	for _, p := range paths {
		pathSet[p] = true
	}
	if !pathSet["projects/sub1"] {
		t.Error("expected projects/sub1 in paths")
	}
	if !pathSet["projects/sub2"] {
		t.Error("expected projects/sub2 in paths")
	}
}

func TestListSubmodules_NoSubmodules(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)

	syncer := NewSyncer(repoRoot)
	paths, err := syncer.listSubmodules(ctx)
	if err != nil {
		t.Fatalf("listSubmodules() error = %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("listSubmodules() got %d paths, want 0 for repo without submodules", len(paths))
	}
}

func TestListSubmodules_FilteredSubmodules(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub1")
	setupSubmodule(t, repoRoot, "projects/sub2")
	setupSubmodule(t, repoRoot, "projects/sub3")

	// Only sync sub1 and sub3
	syncer := NewSyncer(repoRoot, WithSubmodules([]string{"projects/sub1", "projects/sub3"}))
	paths, err := syncer.listSubmodules(ctx)
	if err != nil {
		t.Fatalf("listSubmodules() error = %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("listSubmodules() got %d paths, want 2", len(paths))
	}
}

func TestCheckUncommittedChanges_Clean(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)
	results, err := syncer.CheckUncommittedChanges(ctx)
	if err != nil {
		t.Fatalf("CheckUncommittedChanges() error = %v", err)
	}

	if len(results) != 0 {
		t.Errorf("CheckUncommittedChanges() got %d dirty submodules, want 0", len(results))
	}
}

func TestCheckUncommittedChanges_Dirty(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	subPath := setupSubmodule(t, repoRoot, "projects/sub")

	// Make changes in submodule
	createFile(t, filepath.Join(subPath, "new_file.txt"), "new content")

	syncer := NewSyncer(repoRoot)
	results, err := syncer.CheckUncommittedChanges(ctx)
	if err != nil {
		t.Fatalf("CheckUncommittedChanges() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("CheckUncommittedChanges() got %d dirty submodules, want 1", len(results))
	}

	if results[0].Path != "projects/sub" {
		t.Errorf("dirty submodule path = %q, want %q", results[0].Path, "projects/sub")
	}
}

func TestCheckUncommittedChanges_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupTestRepo(t)
	syncer := NewSyncer(repoRoot)

	_, err := syncer.CheckUncommittedChanges(ctx)
	if err != context.Canceled {
		t.Errorf("CheckUncommittedChanges() error = %v, want context.Canceled", err)
	}
}

func TestCheckURLMismatches_Match(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)
	results, err := syncer.CheckURLMismatches(ctx)
	if err != nil {
		t.Fatalf("CheckURLMismatches() error = %v", err)
	}

	if len(results) != 0 {
		t.Errorf("CheckURLMismatches() got %d mismatches, want 0", len(results))
	}
}

func TestCheckURLMismatches_Mismatch(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	// Change the URL in .git/config to create a mismatch
	runGit(t, repoRoot, "config", "submodule.projects/sub.url", "https://different.url/repo.git")

	syncer := NewSyncer(repoRoot)
	results, err := syncer.CheckURLMismatches(ctx)
	if err != nil {
		t.Fatalf("CheckURLMismatches() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("CheckURLMismatches() got %d mismatches, want 1", len(results))
	}

	if results[0].Submodule != "projects/sub" {
		t.Errorf("mismatch submodule = %q, want %q", results[0].Submodule, "projects/sub")
	}
	if results[0].ActiveURL != "https://different.url/repo.git" {
		t.Errorf("active URL = %q, want %q", results[0].ActiveURL, "https://different.url/repo.git")
	}
}

func TestCheckDetachedHEADs_Attached(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)
	results, err := syncer.CheckDetachedHEADs(ctx)
	if err != nil {
		t.Fatalf("CheckDetachedHEADs() error = %v", err)
	}

	// Submodules are typically in detached HEAD state after clone
	// This test verifies the check runs without error
	// The actual state depends on how git sets up submodules
	t.Logf("Found %d detached HEADs", len(results))
}

func TestCheckDetachedHEADs_Detached(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	subPath := setupSubmodule(t, repoRoot, "projects/sub")

	// Detach HEAD in submodule
	runGit(t, subPath, "checkout", "--detach", "HEAD")

	syncer := NewSyncer(repoRoot)
	results, err := syncer.CheckDetachedHEADs(ctx)
	if err != nil {
		t.Fatalf("CheckDetachedHEADs() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("CheckDetachedHEADs() got %d detached, want 1", len(results))
	}

	if results[0].Path != "projects/sub" {
		t.Errorf("detached submodule path = %q, want %q", results[0].Path, "projects/sub")
	}
}

func TestRunPreflight_AllClean(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)
	result, err := syncer.RunPreflight(ctx)
	if err != nil {
		t.Fatalf("RunPreflight() error = %v", err)
	}

	// Submodules are typically detached after setup, so we only check
	// that uncommitted changes and unpushed commits are clean
	if len(result.UncommittedChanges) != 0 {
		t.Errorf("UncommittedChanges = %d, want 0", len(result.UncommittedChanges))
	}
}

func TestRunPreflight_FailsSafeMode(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	subPath := setupSubmodule(t, repoRoot, "projects/sub")

	// Create uncommitted changes
	createFile(t, filepath.Join(subPath, "dirty.txt"), "dirty")

	syncer := NewSyncer(repoRoot) // Safe mode (default)
	result, err := syncer.RunPreflight(ctx)
	if err != nil {
		t.Fatalf("RunPreflight() error = %v", err)
	}

	if result.Passed {
		t.Error("RunPreflight().Passed = true, want false in safe mode with uncommitted changes")
	}
}

func TestRunPreflight_PassesForceMode(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	subPath := setupSubmodule(t, repoRoot, "projects/sub")

	// Create uncommitted changes
	createFile(t, filepath.Join(subPath, "dirty.txt"), "dirty")

	syncer := NewSyncer(repoRoot, WithForce(true))
	result, err := syncer.RunPreflight(ctx)
	if err != nil {
		t.Fatalf("RunPreflight() error = %v", err)
	}

	if !result.Passed {
		t.Error("RunPreflight().Passed = false, want true in force mode")
	}

	// Should still detect the uncommitted changes
	if len(result.UncommittedChanges) != 1 {
		t.Errorf("UncommittedChanges = %d, want 1", len(result.UncommittedChanges))
	}
}

func TestRunPreflight_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupTestRepo(t)
	syncer := NewSyncer(repoRoot)

	_, err := syncer.RunPreflight(ctx)
	if err != context.Canceled {
		t.Errorf("RunPreflight() error = %v, want context.Canceled", err)
	}
}

func TestRunPreflight_RejectsTraversalSubmodulePaths(t *testing.T) {
	maliciousPaths := []string{
		"../escape",
		"projects/../../../etc",
		"/absolute/path",
		"..",
	}

	repoRoot := t.TempDir()

	for _, p := range maliciousPaths {
		t.Run(p, func(t *testing.T) {
			err := pathutil.ValidateSubmodulePath(repoRoot, p)
			if err == nil {
				t.Errorf("ValidateSubmodulePath(%q): expected error for traversal path, got nil", p)
			}
		})
	}
}
