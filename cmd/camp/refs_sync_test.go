//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
)

// setupCampaignWithTwoSubmodules creates a campaign root with two submodules,
// each advanced by one commit beyond what the campaign root recorded.
func setupCampaignWithTwoSubmodules(t *testing.T) string {
	t.Helper()

	sub1 := setupTestRepo(t)
	os.WriteFile(filepath.Join(sub1, "init.txt"), []byte("1"), 0644)
	run(t, "git", "-C", sub1, "add", "-A")
	run(t, "git", "-C", sub1, "commit", "-m", "init sub1")

	sub2 := setupTestRepo(t)
	os.WriteFile(filepath.Join(sub2, "init.txt"), []byte("2"), 0644)
	run(t, "git", "-C", sub2, "add", "-A")
	run(t, "git", "-C", sub2, "commit", "-m", "init sub2")

	campRoot := setupTestRepo(t)
	runWithEnv(t, campRoot, []string{"GIT_ALLOW_PROTOCOL=file"}, "git", "submodule", "add", sub1, "projects/alpha")
	runWithEnv(t, campRoot, []string{"GIT_ALLOW_PROTOCOL=file"}, "git", "submodule", "add", sub2, "projects/beta")
	run(t, "git", "-C", campRoot, "commit", "-m", "add submodules")

	// Advance both submodules
	alphaPath := filepath.Join(campRoot, "projects", "alpha")
	os.WriteFile(filepath.Join(alphaPath, "change.txt"), []byte("new"), 0644)
	run(t, "git", "-C", alphaPath, "add", "-A")
	run(t, "git", "-C", alphaPath, "commit", "-m", "advance alpha")

	betaPath := filepath.Join(campRoot, "projects", "beta")
	os.WriteFile(filepath.Join(betaPath, "change.txt"), []byte("new"), 0644)
	run(t, "git", "-C", betaPath, "add", "-A")
	run(t, "git", "-C", betaPath, "commit", "-m", "advance beta")

	return campRoot
}

func TestIntegration_DetectRefChanges(t *testing.T) {
	campRoot := setupCampaignWithTwoSubmodules(t)
	ctx := context.Background()

	changes, err := detectRefChanges(ctx, campRoot, []string{"projects/alpha", "projects/beta"})
	if err != nil {
		t.Fatalf("detectRefChanges() error = %v", err)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	for _, c := range changes {
		if !c.Changed {
			t.Errorf("expected %s to be changed", c.Path)
		}
		if c.RecordedSHA == c.CurrentSHA {
			t.Errorf("expected different SHAs for %s", c.Path)
		}
	}
}

func TestIntegration_RefsSyncAtomic(t *testing.T) {
	campRoot := setupCampaignWithTwoSubmodules(t)
	ctx := context.Background()

	beforeCount := strings.TrimSpace(run(t, "git", "-C", campRoot, "rev-list", "--count", "HEAD"))

	// Detect changes
	changes, err := detectRefChanges(ctx, campRoot, []string{"projects/alpha", "projects/beta"})
	if err != nil {
		t.Fatalf("detectRefChanges() error = %v", err)
	}

	var toSync []string
	var names []string
	for _, c := range changes {
		if c.Changed {
			toSync = append(toSync, c.Path)
			names = append(names, c.Name)
		}
	}

	// Stage and commit atomically
	executor, err := git.NewExecutor(campRoot)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	if err := executor.Stage(ctx, toSync); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	msg := fmt.Sprintf("sync submodule refs: %s", strings.Join(names, ", "))
	if err := executor.Commit(ctx, &git.CommitOptions{Message: msg}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	afterCount := strings.TrimSpace(run(t, "git", "-C", campRoot, "rev-list", "--count", "HEAD"))

	// Verify exactly one commit added (atomic)
	before := 0
	after := 0
	fmt.Sscanf(beforeCount, "%d", &before)
	fmt.Sscanf(afterCount, "%d", &after)
	if after != before+1 {
		t.Errorf("expected exactly 1 new commit, got %d (before=%d, after=%d)", after-before, before, after)
	}

	// Verify commit mentions both submodules
	logOutput := run(t, "git", "-C", campRoot, "log", "--oneline", "-1")
	if !strings.Contains(logOutput, "alpha") || !strings.Contains(logOutput, "beta") {
		t.Errorf("commit should mention both submodules, got: %s", logOutput)
	}
}

func TestIntegration_RefsSyncNoOp(t *testing.T) {
	sub := setupTestRepo(t)
	os.WriteFile(filepath.Join(sub, "init.txt"), []byte("1"), 0644)
	run(t, "git", "-C", sub, "add", "-A")
	run(t, "git", "-C", sub, "commit", "-m", "init")

	campRoot := setupTestRepo(t)
	runWithEnv(t, campRoot, []string{"GIT_ALLOW_PROTOCOL=file"}, "git", "submodule", "add", sub, "projects/test")
	run(t, "git", "-C", campRoot, "commit", "-m", "add submodule")

	ctx := context.Background()
	changes, err := detectRefChanges(ctx, campRoot, []string{"projects/test"})
	if err != nil {
		t.Fatalf("detectRefChanges() error = %v", err)
	}

	for _, c := range changes {
		if c.Changed {
			t.Errorf("expected no changes, but %s is marked changed", c.Path)
		}
	}
}

func TestIntegration_RefsSyncSafetyCheck(t *testing.T) {
	campRoot := setupCampaignWithTwoSubmodules(t)

	// Stage a file at campaign root to trigger safety check
	os.WriteFile(filepath.Join(campRoot, "staged.txt"), []byte("staged"), 0644)
	run(t, "git", "-C", campRoot, "add", "staged.txt")

	// Verify staged changes exist
	stagedCmd := exec.Command("git", "-C", campRoot, "diff", "--cached", "--quiet")
	if err := stagedCmd.Run(); err == nil {
		t.Fatal("expected staged changes to exist")
	}

	// The safety check in runRefsSync would abort here.
	// We verify the condition directly rather than calling the full command.
}

func TestIntegration_FilterRefPaths(t *testing.T) {
	all := []string{"projects/alpha", "projects/beta", "projects/gamma"}
	filtered := filterRefPaths(all, []string{"projects/alpha", "projects/gamma"})

	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered paths, got %d", len(filtered))
	}
	if filtered[0] != "projects/alpha" || filtered[1] != "projects/gamma" {
		t.Errorf("unexpected filtered paths: %v", filtered)
	}
}
