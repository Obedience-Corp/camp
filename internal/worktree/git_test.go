package worktree

import (
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	output := `worktree /path/to/main
HEAD abc123
branch refs/heads/main

worktree /path/to/feature
HEAD def456
branch refs/heads/feature
locked

worktree /path/to/stale
HEAD 789abc
detached
prunable gitdir file points to non-existent location
`

	entries := parseWorktreeList(output)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	// Check main worktree
	if entries[0].Path != "/path/to/main" {
		t.Errorf("entries[0].Path = %q, want /path/to/main", entries[0].Path)
	}
	if entries[0].Branch != "main" {
		t.Errorf("entries[0].Branch = %q, want main", entries[0].Branch)
	}
	if entries[0].Commit != "abc123" {
		t.Errorf("entries[0].Commit = %q, want abc123", entries[0].Commit)
	}
	if entries[0].IsLocked {
		t.Error("entries[0] should not be locked")
	}

	// Check locked worktree
	if entries[1].Path != "/path/to/feature" {
		t.Errorf("entries[1].Path = %q, want /path/to/feature", entries[1].Path)
	}
	if entries[1].Branch != "feature" {
		t.Errorf("entries[1].Branch = %q, want feature", entries[1].Branch)
	}
	if !entries[1].IsLocked {
		t.Error("entries[1] should be locked")
	}

	// Check prunable worktree
	if entries[2].Path != "/path/to/stale" {
		t.Errorf("entries[2].Path = %q, want /path/to/stale", entries[2].Path)
	}
	if entries[2].Prunable == "" {
		t.Error("entries[2] should be prunable")
	}
	if entries[2].Branch != "HEAD (detached)" {
		t.Errorf("entries[2].Branch = %q, want HEAD (detached)", entries[2].Branch)
	}
}

func TestParseWorktreeList_Empty(t *testing.T) {
	entries := parseWorktreeList("")
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestParseWorktreeList_Single(t *testing.T) {
	output := `worktree /path/to/main
HEAD abc123
branch refs/heads/main
`

	entries := parseWorktreeList(output)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	if entries[0].Path != "/path/to/main" {
		t.Errorf("entries[0].Path = %q, want /path/to/main", entries[0].Path)
	}
}

func TestParseWorktreeList_Bare(t *testing.T) {
	output := `worktree /path/to/bare.git
bare

worktree /path/to/main
HEAD abc123
branch refs/heads/main
`

	entries := parseWorktreeList(output)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	if !entries[0].IsBare {
		t.Error("entries[0] should be bare")
	}
	if entries[1].IsBare {
		t.Error("entries[1] should not be bare")
	}
}

func TestGitError(t *testing.T) {
	err := &gitError{
		cause:  nil,
		output: "fatal: not a git repository",
	}

	if err.Error() != "fatal: not a git repository" {
		t.Errorf("Error() = %q, want fatal: not a git repository", err.Error())
	}

	if err.Unwrap() != nil {
		t.Error("Unwrap() should return nil")
	}
}

func TestParseGitError(t *testing.T) {
	t.Run("with output", func(t *testing.T) {
		err := parseGitError(nil, []byte("fatal: error message\n"))
		gitErr, ok := err.(*gitError)
		if !ok {
			t.Fatal("expected *gitError")
		}
		if gitErr.output != "fatal: error message" {
			t.Errorf("output = %q", gitErr.output)
		}
	})

	t.Run("without output", func(t *testing.T) {
		original := &gitError{output: "original"}
		err := parseGitError(original, nil)
		if err != original {
			t.Error("should return original error when no output")
		}
	})
}

func TestNewGitWorktree(t *testing.T) {
	gw := NewGitWorktree("/path/to/project")
	if gw.projectPath != "/path/to/project" {
		t.Errorf("projectPath = %q, want /path/to/project", gw.projectPath)
	}
	if gw.timeout != 30*1e9 { // 30 seconds in nanoseconds
		t.Errorf("timeout = %v, want 30s", gw.timeout)
	}
}

func TestGitWorktree_WithTimeout(t *testing.T) {
	gw := NewGitWorktree("/path/to/project").WithTimeout(60 * 1e9)
	if gw.timeout != 60*1e9 {
		t.Errorf("timeout = %v, want 60s", gw.timeout)
	}
}
