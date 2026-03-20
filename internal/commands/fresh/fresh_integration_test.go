//go:build integration

package fresh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// run executes a command and fails the test on error.
func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nOutput: %s", name, args, err, output)
	}
	return string(output)
}

// runWithEnv executes a command with custom environment in a directory.
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
	campDir := t.TempDir()
	run(t, "git", "init", campDir)
	run(t, "git", "-C", campDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", campDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(campDir, "README.md"), []byte("# Campaign"), 0644)
	run(t, "git", "-C", campDir, "add", ".")
	run(t, "git", "-C", campDir, "commit", "-m", "Initial campaign commit")

	// Add submodule under projects/
	runWithEnv(t, campDir, []string{"GIT_ALLOW_PROTOCOL=file"},
		"git", "submodule", "add", bareDir, "projects/test-project")
	run(t, "git", "-C", campDir, "commit", "-m", "Add submodule")

	return campDir, bareDir
}

func setupCampaignWithNestedSubmoduleProject(t *testing.T) string {
	t.Helper()

	nestedBare := t.TempDir()
	run(t, "git", "init", "--bare", nestedBare)

	nestedSeed := t.TempDir()
	run(t, "git", "clone", nestedBare, nestedSeed)
	run(t, "git", "-C", nestedSeed, "config", "user.email", "test@test.com")
	run(t, "git", "-C", nestedSeed, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(nestedSeed, "README.md"), []byte("# Nested Project"), 0o644); err != nil {
		t.Fatalf("failed to write nested README: %v", err)
	}
	run(t, "git", "-C", nestedSeed, "add", ".")
	run(t, "git", "-C", nestedSeed, "commit", "-m", "Initial nested commit")
	run(t, "git", "-C", nestedSeed, "push", "origin", "main")

	projectBare := t.TempDir()
	run(t, "git", "init", "--bare", projectBare)

	projectSeed := t.TempDir()
	run(t, "git", "clone", projectBare, projectSeed)
	run(t, "git", "-C", projectSeed, "config", "user.email", "test@test.com")
	run(t, "git", "-C", projectSeed, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(projectSeed, "README.md"), []byte("# Monorepo Project"), 0o644); err != nil {
		t.Fatalf("failed to write monorepo README: %v", err)
	}
	run(t, "git", "-C", projectSeed, "add", ".")
	run(t, "git", "-C", projectSeed, "commit", "-m", "Initial project commit")
	runWithEnv(t, projectSeed, []string{"GIT_ALLOW_PROTOCOL=file"},
		"git", "submodule", "add", nestedBare, "vendor/tool")
	run(t, "git", "-C", projectSeed, "commit", "-m", "Add nested submodule")
	run(t, "git", "-C", projectSeed, "push", "origin", "main")

	campDir := t.TempDir()
	run(t, "git", "init", campDir)
	run(t, "git", "-C", campDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", campDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(campDir, "README.md"), []byte("# Campaign"), 0o644); err != nil {
		t.Fatalf("failed to write campaign README: %v", err)
	}
	run(t, "git", "-C", campDir, "add", ".")
	run(t, "git", "-C", campDir, "commit", "-m", "Initial campaign commit")
	runWithEnv(t, campDir, []string{"GIT_ALLOW_PROTOCOL=file"},
		"git", "submodule", "add", projectBare, "projects/test-project")
	run(t, "git", "-C", campDir, "commit", "-m", "Add monorepo project")

	projectDir := filepath.Join(campDir, "projects", "test-project")
	runWithEnv(t, projectDir, []string{"GIT_ALLOW_PROTOCOL=file"},
		"git", "submodule", "update", "--init", "--recursive")

	return campDir
}

func TestIntegration_ExecuteFresh_CreatesAndPushesNewBranch(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	subDir := filepath.Join(campDir, "projects", "test-project")

	err := executeFresh(context.Background(), "test-project", subDir, freshOptions{
		branch:      "feat/new-work",
		prune:       true,
		pruneRemote: true,
		push:        true,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	current := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != "feat/new-work" {
		t.Fatalf("current branch = %q, want %q", current, "feat/new-work")
	}

	upstream := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "@{upstream}"))
	if upstream != "origin/feat/new-work" {
		t.Fatalf("upstream = %q, want %q", upstream, "origin/feat/new-work")
	}
}

func TestIntegration_ExecuteFresh_DoesNotPushExistingBranch(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	subDir := filepath.Join(campDir, "projects", "test-project")

	run(t, "git", "-C", subDir, "checkout", "-b", "develop")
	if err := os.WriteFile(filepath.Join(subDir, "develop.txt"), []byte("develop"), 0o644); err != nil {
		t.Fatalf("failed to write develop.txt: %v", err)
	}
	run(t, "git", "-C", subDir, "add", ".")
	run(t, "git", "-C", subDir, "commit", "-m", "Develop work")
	run(t, "git", "-C", subDir, "checkout", "main")

	err := executeFresh(context.Background(), "test-project", subDir, freshOptions{
		branch:      "develop",
		prune:       true,
		pruneRemote: true,
		push:        true,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	current := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != "main" {
		t.Fatalf("current branch = %q, want %q", current, "main")
	}

	cmd := exec.Command("git", "-C", subDir, "rev-parse", "--abbrev-ref", "develop@{upstream}")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected existing branch to remain without upstream, got %s", strings.TrimSpace(string(output)))
	}
}

func TestIntegration_ExecuteFresh_HandlesDefaultBranchInAnotherWorktree(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	subDir := filepath.Join(campDir, "projects", "test-project")

	run(t, "git", "-C", subDir, "checkout", "-b", "feature-merged")
	if err := os.WriteFile(filepath.Join(subDir, "feature.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatalf("failed to write feature.txt: %v", err)
	}
	run(t, "git", "-C", subDir, "add", ".")
	run(t, "git", "-C", subDir, "commit", "-m", "Feature work")

	mainWorktree := t.TempDir()
	run(t, "git", "-C", subDir, "worktree", "add", mainWorktree, "main")

	stableWorktree := t.TempDir()
	run(t, "git", "-C", subDir, "worktree", "add", "-b", "stable-v0.1.2", stableWorktree, "main")
	if err := os.WriteFile(filepath.Join(stableWorktree, "release.txt"), []byte("release"), 0o644); err != nil {
		t.Fatalf("failed to write release.txt: %v", err)
	}
	run(t, "git", "-C", stableWorktree, "add", ".")
	run(t, "git", "-C", stableWorktree, "commit", "-m", "Release branch work")

	mergedSiblingWorktree := t.TempDir()
	run(t, "git", "-C", subDir, "worktree", "add", "-b", "feature-sidecar", mergedSiblingWorktree, "main")
	if err := os.WriteFile(filepath.Join(mergedSiblingWorktree, "sidecar.txt"), []byte("sidecar"), 0o644); err != nil {
		t.Fatalf("failed to write sidecar.txt: %v", err)
	}
	run(t, "git", "-C", mergedSiblingWorktree, "add", ".")
	run(t, "git", "-C", mergedSiblingWorktree, "commit", "-m", "Sidecar work")

	run(t, "git", "-C", mainWorktree, "merge", "feature-merged")
	run(t, "git", "-C", mainWorktree, "merge", "feature-sidecar")
	run(t, "git", "-C", mainWorktree, "push", "origin", "main")

	err := executeFresh(context.Background(), "test-project", subDir, freshOptions{
		branch: "develop",
		prune:  true,
		push:   false,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	current := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != "develop" {
		t.Fatalf("current branch = %q, want %q", current, "develop")
	}

	cmd := exec.Command("git", "-C", subDir, "rev-parse", "--verify", "--quiet", "refs/heads/feature-merged")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected merged branch to be deleted, got %s", strings.TrimSpace(string(output)))
	}

	sidecarRef := exec.Command("git", "-C", subDir, "rev-parse", "--verify", "--quiet", "refs/heads/feature-sidecar")
	if output, err := sidecarRef.CombinedOutput(); err == nil {
		t.Fatalf("expected merged sibling worktree branch to be deleted, got %s", strings.TrimSpace(string(output)))
	}

	stableRef := exec.Command("git", "-C", subDir, "rev-parse", "--verify", "--quiet", "refs/heads/stable-v0.1.2")
	if output, err := stableRef.CombinedOutput(); err != nil {
		t.Fatalf("expected stable worktree branch to remain, err=%v output=%s", err, strings.TrimSpace(string(output)))
	}

	worktrees := run(t, "git", "-C", subDir, "worktree", "list", "--porcelain")
	if !strings.Contains(worktrees, mainWorktree) {
		t.Fatalf("expected main worktree to remain listed, got:\n%s", worktrees)
	}
	if !strings.Contains(worktrees, stableWorktree) {
		t.Fatalf("expected stable worktree to remain listed, got:\n%s", worktrees)
	}
	if strings.Contains(worktrees, mergedSiblingWorktree) {
		t.Fatalf("expected merged sibling worktree to be removed, got:\n%s", worktrees)
	}
}

func TestIntegration_ExecuteFresh_IgnoresNestedSubmoduleRefDrift(t *testing.T) {
	campDir := setupCampaignWithNestedSubmoduleProject(t)
	projectDir := filepath.Join(campDir, "projects", "test-project")
	nestedDir := filepath.Join(projectDir, "vendor", "tool")

	run(t, "git", "-C", nestedDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", nestedDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(nestedDir, "drift.txt"), []byte("drift"), 0o644); err != nil {
		t.Fatalf("failed to write drift.txt: %v", err)
	}
	run(t, "git", "-C", nestedDir, "add", ".")
	run(t, "git", "-C", nestedDir, "commit", "-m", "Nested drift")

	status := run(t, "git", "-C", projectDir, "status", "--short", "--ignore-submodules=none")
	if !strings.Contains(status, "vendor/tool") {
		t.Fatalf("expected monorepo status to show nested submodule drift, got:\n%s", status)
	}

	err := executeFresh(context.Background(), "test-project", projectDir, freshOptions{
		branch: "develop",
		prune:  false,
		push:   false,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	current := strings.TrimSpace(run(t, "git", "-C", projectDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != "develop" {
		t.Fatalf("current branch = %q, want %q", current, "develop")
	}
}

func TestIntegration_ExecuteFresh_DoesNotDeleteRemoteBranches(t *testing.T) {
	campDir, bareDir := setupCampaignWithSubmodule(t)
	subDir := filepath.Join(campDir, "projects", "test-project")

	run(t, "git", "-C", subDir, "checkout", "-b", "feature-remote")
	if err := os.WriteFile(filepath.Join(subDir, "feature-remote.txt"), []byte("feature remote"), 0o644); err != nil {
		t.Fatalf("failed to write feature-remote.txt: %v", err)
	}
	run(t, "git", "-C", subDir, "add", ".")
	run(t, "git", "-C", subDir, "commit", "-m", "Feature remote work")
	run(t, "git", "-C", subDir, "push", "-u", "origin", "feature-remote")

	mainWorktree := t.TempDir()
	run(t, "git", "-C", subDir, "worktree", "add", mainWorktree, "main")
	run(t, "git", "-C", mainWorktree, "merge", "feature-remote")
	run(t, "git", "-C", mainWorktree, "push", "origin", "main")

	err := executeFresh(context.Background(), "test-project", subDir, freshOptions{
		prune:       true,
		pruneRemote: true,
		push:        false,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	remoteHeads := run(t, "git", "--git-dir", bareDir, "show-ref", "--verify", "refs/heads/feature-remote")
	if !strings.Contains(remoteHeads, "refs/heads/feature-remote") {
		t.Fatalf("expected remote feature branch to remain, got:\n%s", remoteHeads)
	}
}
