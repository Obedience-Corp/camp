package sync

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSync_CleanRepo(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Success {
		t.Error("Sync().Success = false, want true for clean repo")
	}
	if !result.PreflightPassed {
		t.Error("Sync().PreflightPassed = false, want true")
	}
}

func TestSync_DryRun(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	// Create a URL mismatch
	runGit(t, repoRoot, "config", "submodule.projects/sub.url", "https://different.url/repo.git")

	syncer := NewSyncer(repoRoot, WithDryRun(true))
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Success {
		t.Error("Sync().Success = false, want true in dry-run mode")
	}

	// Should report the URL change that would happen
	if len(result.URLChanges) != 1 {
		t.Errorf("URLChanges = %d, want 1 in dry-run mode", len(result.URLChanges))
	}

	// Verify the URL wasn't actually changed
	cmd := exec.Command("git", "-C", repoRoot, "config", "submodule.projects/sub.url")
	output, _ := cmd.Output()
	if string(output) != "https://different.url/repo.git\n" {
		t.Error("dry-run mode should not modify URLs")
	}
}

func TestSync_ForceMode(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	subPath := setupSubmodule(t, repoRoot, "projects/sub")

	// Create uncommitted changes
	createFile(t, filepath.Join(subPath, "dirty.txt"), "dirty content")

	// Without force, should fail
	syncer := NewSyncer(repoRoot)
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Success {
		t.Error("Sync().Success = true, want false without force mode")
	}

	// With force, should succeed
	syncer = NewSyncer(repoRoot, WithForce(true))
	result, err = syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if !result.Success {
		t.Error("Sync().Success = false, want true with force mode")
	}

	// Should have warnings about uncommitted changes
	hasWarning := false
	for _, w := range result.Warnings {
		if contains(w, "uncommitted") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected warning about uncommitted changes in force mode")
	}
}

func TestSync_URLMismatch(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	// Get the original URL
	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/sub.url")
	originalURL, _ := cmd.Output()

	// Create a URL mismatch by changing .git/config
	runGit(t, repoRoot, "config", "submodule.projects/sub.url", "https://different.url/repo.git")

	syncer := NewSyncer(repoRoot)
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Success {
		t.Error("Sync().Success = false, want true")
	}

	// Should have fixed the URL
	cmd = exec.Command("git", "-C", repoRoot, "config", "submodule.projects/sub.url")
	newURL, _ := cmd.Output()
	if string(newURL) != string(originalURL) {
		t.Errorf("URL not synced: got %q, want %q", string(newURL), string(originalURL))
	}
}

func TestSync_ParallelJobs(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub1")
	setupSubmodule(t, repoRoot, "projects/sub2")

	syncer := NewSyncer(repoRoot, WithParallel(4))
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Success {
		t.Error("Sync().Success = false, want true")
	}

	// Both submodules should be in results
	if len(result.UpdateResults) != 2 {
		t.Errorf("UpdateResults = %d, want 2", len(result.UpdateResults))
	}
}

func TestSync_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupTestRepo(t)
	syncer := NewSyncer(repoRoot)

	_, err := syncer.Sync(ctx)
	if err != context.Canceled {
		t.Errorf("Sync() error = %v, want context.Canceled", err)
	}
}

func TestSyncURLs(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	// Get the original URL from .gitmodules
	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/sub.url")
	declaredURL, _ := cmd.Output()

	// Create a URL mismatch
	runGit(t, repoRoot, "config", "submodule.projects/sub.url", "https://old.url/repo.git")

	syncer := NewSyncer(repoRoot)
	changes, err := syncer.syncURLs(ctx)
	if err != nil {
		t.Fatalf("syncURLs() error = %v", err)
	}

	// Should detect the URL change
	if len(changes) != 1 {
		t.Fatalf("syncURLs() changes = %d, want 1", len(changes))
	}

	if changes[0].OldURL != "https://old.url/repo.git" {
		t.Errorf("OldURL = %q, want %q", changes[0].OldURL, "https://old.url/repo.git")
	}

	// Verify URL was actually synced
	cmd = exec.Command("git", "-C", repoRoot, "config", "submodule.projects/sub.url")
	activeURL, _ := cmd.Output()
	if string(activeURL) != string(declaredURL) {
		t.Errorf("URL not synced: got %q, want %q", string(activeURL), string(declaredURL))
	}
}

func TestValidateUpdate_AllInitialized(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)

	// First do a proper sync to ensure everything is initialized
	_, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Now validate should pass
	err = syncer.validateUpdate(ctx)
	if err != nil {
		t.Errorf("validateUpdate() error = %v, want nil", err)
	}
}

func TestVerifySubmodules(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)

	// Run sync first to initialize
	_, _ = syncer.Sync(ctx)

	results, err := syncer.verifySubmodules(ctx)
	if err != nil {
		t.Fatalf("verifySubmodules() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("verifySubmodules() results = %d, want 1", len(results))
	}

	if !results[0].Success {
		t.Errorf("submodule Success = false, want true")
	}
}

func TestCollectWarnings(t *testing.T) {
	syncer := NewSyncer("/tmp/test", WithForce(true))

	preflight := &PreflightResult{
		UncommittedChanges: []SubmoduleStatus{
			{Path: "projects/dirty", Details: "2 files"},
		},
		DetachedHEADs: []DetachedHEADStatus{
			{Path: "projects/detached", LocalCommits: 3, HasLocalWork: true},
		},
	}

	warnings := syncer.collectWarnings(preflight)

	if len(warnings) != 2 {
		t.Fatalf("collectWarnings() = %d warnings, want 2", len(warnings))
	}

	// Check for uncommitted changes warning
	hasUncommitted := false
	hasDetached := false
	for _, w := range warnings {
		if contains(w, "uncommitted") && contains(w, "projects/dirty") {
			hasUncommitted = true
		}
		if contains(w, "detached HEAD") && contains(w, "projects/detached") {
			hasDetached = true
		}
	}

	if !hasUncommitted {
		t.Error("expected uncommitted changes warning")
	}
	if !hasDetached {
		t.Error("expected detached HEAD warning")
	}
}

func TestCollectWarnings_SafeMode(t *testing.T) {
	// In safe mode (no force), uncommitted changes should NOT become warnings
	// because they cause the sync to abort
	syncer := NewSyncer("/tmp/test") // No force

	preflight := &PreflightResult{
		UncommittedChanges: []SubmoduleStatus{
			{Path: "projects/dirty", Details: "2 files"},
		},
	}

	warnings := syncer.collectWarnings(preflight)

	if len(warnings) != 0 {
		t.Errorf("collectWarnings() in safe mode = %d warnings, want 0", len(warnings))
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
