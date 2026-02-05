package clone

import (
	"context"
	"testing"
)

func TestParseGitmodules_NoSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)

	submodules, err := parseGitmodules(ctx, repoDir)
	if err != nil {
		t.Fatalf("parseGitmodules() error = %v", err)
	}

	if len(submodules) != 0 {
		t.Errorf("parseGitmodules() returned %d submodules, want 0", len(submodules))
	}
}

func TestParseGitmodules_SingleSubmodule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	submodules, err := parseGitmodules(ctx, repoDir)
	if err != nil {
		t.Fatalf("parseGitmodules() error = %v", err)
	}

	if len(submodules) != 1 {
		t.Fatalf("parseGitmodules() returned %d submodules, want 1", len(submodules))
	}

	sub := submodules[0]
	if sub.Path != "projects/sub" {
		t.Errorf("submodule path = %q, want 'projects/sub'", sub.Path)
	}
	if sub.URL == "" {
		t.Error("submodule URL is empty")
	}
}

func TestParseGitmodules_MultipleSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub1")
	setupSubmodule(t, repoDir, "projects/sub2")
	setupSubmodule(t, repoDir, "libs/shared")

	submodules, err := parseGitmodules(ctx, repoDir)
	if err != nil {
		t.Fatalf("parseGitmodules() error = %v", err)
	}

	if len(submodules) != 3 {
		t.Errorf("parseGitmodules() returned %d submodules, want 3", len(submodules))
	}

	// Verify all paths are present
	paths := make(map[string]bool)
	for _, sub := range submodules {
		paths[sub.Path] = true
	}

	expectedPaths := []string{"projects/sub1", "projects/sub2", "libs/shared"}
	for _, p := range expectedPaths {
		if !paths[p] {
			t.Errorf("parseGitmodules() missing path %q", p)
		}
	}
}

func TestParseGitmodules_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := parseGitmodules(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("parseGitmodules() error = %v, want context.Canceled", err)
	}
}
