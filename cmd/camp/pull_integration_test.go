//go:build integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// setupCampaignWithSubmodule creates a campaign repo with a submodule
// under projects/ that has a bare remote origin. Returns (campaignDir, bareRemoteDir).
func setupCampaignWithSubmodule(t *testing.T) (string, string) {
	t.Helper()

	// Create bare remote for the submodule
	bareDir := t.TempDir()
	run(t, "git", "init", "--bare", bareDir)

	// Clone bare to create initial content, then push
	cloneDir := t.TempDir()
	run(t, "git", "clone", bareDir, cloneDir)
	run(t, "git", "-C", cloneDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", cloneDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(cloneDir, "README.md"), []byte("# Test Project"), 0644)
	run(t, "git", "-C", cloneDir, "add", ".")
	run(t, "git", "-C", cloneDir, "commit", "-m", "Initial commit")
	run(t, "git", "-C", cloneDir, "push", "origin", "main")

	// Create campaign repo
	campDir := setupTestRepo(t)
	os.WriteFile(filepath.Join(campDir, "README.md"), []byte("# Campaign"), 0644)
	run(t, "git", "-C", campDir, "add", ".")
	run(t, "git", "-C", campDir, "commit", "-m", "Initial campaign commit")

	// Add submodule under projects/
	runWithEnv(t, campDir, []string{"GIT_ALLOW_PROTOCOL=file"},
		"git", "submodule", "add", bareDir, "projects/test-project")
	run(t, "git", "-C", campDir, "commit", "-m", "Add submodule")

	return campDir, bareDir
}

// pushCommitToBare creates a fresh clone of bareDir, commits a file, and pushes.
func pushCommitToBare(t *testing.T, bareDir, filename, content, message string) {
	t.Helper()

	cloneDir := t.TempDir()
	run(t, "git", "clone", bareDir, cloneDir)
	run(t, "git", "-C", cloneDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", cloneDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(cloneDir, filename), []byte(content), 0644)
	run(t, "git", "-C", cloneDir, "add", ".")
	run(t, "git", "-C", cloneDir, "commit", "-m", message)
	run(t, "git", "-C", cloneDir, "push", "origin", "main")
}

func TestIntegration_PullAll_UpToDate(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	ctx := context.Background()

	// Everything is up-to-date, should succeed with no errors
	err := runPullAll(ctx, campDir, nil)
	if err != nil {
		t.Fatalf("runPullAll() error = %v", err)
	}
}

func TestIntegration_PullAll_PullsNewCommits(t *testing.T) {
	campDir, bareDir := setupCampaignWithSubmodule(t)
	ctx := context.Background()

	// Push a new commit to the bare remote
	pushCommitToBare(t, bareDir, "new-file.txt", "new content", "Add new file")

	// Pull should succeed
	err := runPullAll(ctx, campDir, nil)
	if err != nil {
		t.Fatalf("runPullAll() error = %v", err)
	}

	// Verify the new file was pulled into the submodule
	newFile := filepath.Join(campDir, "projects", "test-project", "new-file.txt")
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		t.Error("new-file.txt was not pulled into submodule")
	}
}

func TestIntegration_PullAll_SkipsDetachedHEAD(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	ctx := context.Background()

	// Detach HEAD in the submodule
	subDir := filepath.Join(campDir, "projects", "test-project")
	hash := run(t, "git", "-C", subDir, "rev-parse", "HEAD")
	run(t, "git", "-C", subDir, "checkout", hash[:8])

	// Should succeed (skips detached HEAD, doesn't fail)
	err := runPullAll(ctx, campDir, nil)
	if err != nil {
		t.Fatalf("runPullAll() error = %v", err)
	}
}

func TestIntegration_PullAll_SkipsNoUpstream(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	ctx := context.Background()

	// Create a local-only branch with no upstream tracking
	subDir := filepath.Join(campDir, "projects", "test-project")
	run(t, "git", "-C", subDir, "checkout", "-b", "local-only")

	// Should succeed (skips no-upstream repos)
	err := runPullAll(ctx, campDir, nil)
	if err != nil {
		t.Fatalf("runPullAll() error = %v", err)
	}
}

func TestIntegration_PullAll_ContextCancellation(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := runPullAll(ctx, campDir, nil)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestIntegration_PullAll_PassesThroughGitFlags(t *testing.T) {
	campDir, bareDir := setupCampaignWithSubmodule(t)
	ctx := context.Background()

	// Push a new commit to the bare remote
	pushCommitToBare(t, bareDir, "ff-file.txt", "ff content", "Fast-forward commit")

	// Pull with --ff-only should succeed (this is a fast-forward)
	err := runPullAll(ctx, campDir, []string{"--ff-only"})
	if err != nil {
		t.Fatalf("runPullAll(--ff-only) error = %v", err)
	}

	// Verify the file was pulled
	ffFile := filepath.Join(campDir, "projects", "test-project", "ff-file.txt")
	if _, err := os.Stat(ffFile); os.IsNotExist(err) {
		t.Error("ff-file.txt was not pulled with --ff-only")
	}
}
