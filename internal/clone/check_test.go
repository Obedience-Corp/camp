package clone

import (
	"context"
	"path/filepath"
	"testing"
)

func TestInitializedCheck_ID(t *testing.T) {
	check := &InitializedCheck{}
	if check.ID() != "initialized" {
		t.Errorf("ID() = %q, want 'initialized'", check.ID())
	}
}

func TestInitializedCheck_Name(t *testing.T) {
	check := &InitializedCheck{}
	if check.Name() != "Submodule Initialization" {
		t.Errorf("Name() = %q, want 'Submodule Initialization'", check.Name())
	}
}

func TestInitializedCheck_Run_NoSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)

	check := &InitializedCheck{}
	issues, err := check.Run(ctx, repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("Run() returned %d issues, want 0 for repo without submodules", len(issues))
	}
}

func TestInitializedCheck_Run_Initialized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	check := &InitializedCheck{}
	issues, err := check.Run(ctx, repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("Run() returned %d issues, want 0 for initialized submodule", len(issues))
	}
}

func TestInitializedCheck_Run_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := &InitializedCheck{}
	_, err := check.Run(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestCommitCheck_ID(t *testing.T) {
	check := &CommitCheck{}
	if check.ID() != "commits" {
		t.Errorf("ID() = %q, want 'commits'", check.ID())
	}
}

func TestCommitCheck_Name(t *testing.T) {
	check := &CommitCheck{}
	if check.Name() != "Commit References" {
		t.Errorf("Name() = %q, want 'Commit References'", check.Name())
	}
}

func TestCommitCheck_Run_NoSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)

	check := &CommitCheck{}
	issues, err := check.Run(ctx, repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("Run() returned %d issues, want 0 for repo without submodules", len(issues))
	}
}

func TestCommitCheck_Run_CorrectCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	check := &CommitCheck{}
	issues, err := check.Run(ctx, repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("Run() returned %d issues, want 0 for correct commits", len(issues))
	}
}

func TestCommitCheck_Run_WrongCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	subPath := setupSubmodule(t, repoDir, "projects/sub")

	// Make a new commit in the submodule
	createFile(t, filepath.Join(subPath, "new_file.txt"), "new content")
	runGit(t, subPath, "add", ".")
	runGit(t, subPath, "commit", "-m", "New commit in submodule")

	check := &CommitCheck{}
	issues, err := check.Run(ctx, repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Should detect the wrong commit (warning)
	if len(issues) != 1 {
		t.Fatalf("Run() returned %d issues, want 1", len(issues))
	}
	if issues[0].Severity != SeverityWarning {
		t.Errorf("Issue severity = %v, want warning", issues[0].Severity)
	}
}

func TestCommitCheck_Run_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := &CommitCheck{}
	_, err := check.Run(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestURLMatchCheck_ID(t *testing.T) {
	check := &URLMatchCheck{}
	if check.ID() != "urls" {
		t.Errorf("ID() = %q, want 'urls'", check.ID())
	}
}

func TestURLMatchCheck_Name(t *testing.T) {
	check := &URLMatchCheck{}
	if check.Name() != "URL Consistency" {
		t.Errorf("Name() = %q, want 'URL Consistency'", check.Name())
	}
}

func TestURLMatchCheck_Run_NoSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)

	check := &URLMatchCheck{}
	issues, err := check.Run(ctx, repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("Run() returned %d issues, want 0 for repo without submodules", len(issues))
	}
}

func TestURLMatchCheck_Run_MatchingURLs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	check := &URLMatchCheck{}
	issues, err := check.Run(ctx, repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("Run() returned %d issues, want 0 for matching URLs", len(issues))
	}
}

func TestURLMatchCheck_Run_MismatchedURLs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	// Change URL in .git/config to create mismatch
	runGit(t, repoDir, "config", "submodule.projects/sub.url", "https://different.url/repo.git")

	check := &URLMatchCheck{}
	issues, err := check.Run(ctx, repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("Run() returned %d issues, want 1", len(issues))
	}
	if issues[0].Severity != SeverityWarning {
		t.Errorf("Issue severity = %v, want warning", issues[0].Severity)
	}
	if issues[0].FixCommand == "" {
		t.Error("Issue should have fix command")
	}
}

func TestURLMatchCheck_Run_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := &URLMatchCheck{}
	_, err := check.Run(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}
