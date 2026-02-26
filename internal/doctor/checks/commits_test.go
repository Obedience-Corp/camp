package checks

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/doctor"
)

func TestCommitsCheck_ID(t *testing.T) {
	check := NewCommitsCheck()
	if got := check.ID(); got != "commits" {
		t.Errorf("ID() = %q, want %q", got, "commits")
	}
}

func TestCommitsCheck_Name(t *testing.T) {
	check := NewCommitsCheck()
	if got := check.Name(); got == "" {
		t.Error("Name() returned empty string")
	}
}

func TestCommitsCheck_Description(t *testing.T) {
	check := NewCommitsCheck()
	if got := check.Description(); got == "" {
		t.Error("Description() returned empty string")
	}
}

func TestCommitsCheck_Run_NoSubmodules(t *testing.T) {
	// Create a temp git repo with no submodules
	repoDir := t.TempDir()
	mustRunGit(t, repoDir, "init")

	check := NewCommitsCheck()
	result, err := check.Run(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed to be true for repo with no submodules")
	}
	if result.Total != 0 {
		t.Errorf("expected Total = 0, got %d", result.Total)
	}
}

func TestCommitsCheck_Run_AlignedCommits(t *testing.T) {
	// Create a parent repo with a submodule where commits are aligned
	parentDir := t.TempDir()
	submoduleDir := t.TempDir()

	// Create the "remote" submodule repo
	mustRunGit(t, submoduleDir, "init")
	mustRunGit(t, submoduleDir, "config", "user.email", "test@test.com")
	mustRunGit(t, submoduleDir, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(submoduleDir, "README.md"), "# Test")
	mustRunGit(t, submoduleDir, "add", ".")
	mustRunGit(t, submoduleDir, "commit", "-m", "Initial commit")

	// Create the parent repo
	mustRunGit(t, parentDir, "init")
	mustRunGit(t, parentDir, "config", "user.email", "test@test.com")
	mustRunGit(t, parentDir, "config", "user.name", "Test")

	// Add submodule
	mustRunGit(t, parentDir, "submodule", "add", submoduleDir, "projects/sub")
	mustRunGit(t, parentDir, "commit", "-m", "Add submodule")

	// Run check - commits should be aligned
	check := NewCommitsCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed to be true for aligned commits")
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %d", len(result.Issues))
	}
}

func TestCommitsCheck_Run_SubmoduleAhead(t *testing.T) {
	// Create a parent repo with a submodule that has new local commits
	parentDir := t.TempDir()
	submoduleDir := t.TempDir()

	// Create the "remote" submodule repo
	mustRunGit(t, submoduleDir, "init")
	mustRunGit(t, submoduleDir, "config", "user.email", "test@test.com")
	mustRunGit(t, submoduleDir, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(submoduleDir, "README.md"), "# Test")
	mustRunGit(t, submoduleDir, "add", ".")
	mustRunGit(t, submoduleDir, "commit", "-m", "Initial commit")

	// Create the parent repo
	mustRunGit(t, parentDir, "init")
	mustRunGit(t, parentDir, "config", "user.email", "test@test.com")
	mustRunGit(t, parentDir, "config", "user.name", "Test")

	// Add submodule
	mustRunGit(t, parentDir, "submodule", "add", submoduleDir, "projects/sub")
	mustRunGit(t, parentDir, "commit", "-m", "Add submodule")

	// Make a new commit in the submodule (ahead of what parent expects)
	subPath := filepath.Join(parentDir, "projects/sub")
	mustRunGit(t, subPath, "config", "user.email", "test@test.com")
	mustRunGit(t, subPath, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(subPath, "new.txt"), "new content")
	mustRunGit(t, subPath, "add", ".")
	mustRunGit(t, subPath, "commit", "-m", "New local commit")

	// Run check - should detect submodule is ahead
	check := NewCommitsCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false when submodule is ahead")
	}
	if len(result.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(result.Issues))
	}

	// Verify issue details
	if len(result.Issues) > 0 {
		issue := result.Issues[0]
		if issue.Severity != doctor.SeverityWarning {
			t.Errorf("expected SeverityWarning, got %v", issue.Severity)
		}
		if issue.CheckID != "commits" {
			t.Errorf("expected CheckID 'commits', got %q", issue.CheckID)
		}
		if issue.AutoFixable {
			t.Error("expected AutoFixable to be false when submodule is ahead")
		}
		if issue.Details["status"] != "ahead" {
			t.Errorf("expected status 'ahead', got %v", issue.Details["status"])
		}
	}
}

func TestCommitsCheck_Run_SubmoduleBehind(t *testing.T) {
	// Create a parent repo with a submodule that is behind
	parentDir := t.TempDir()
	submoduleDir := t.TempDir()

	// Create the "remote" submodule repo
	mustRunGit(t, submoduleDir, "init")
	mustRunGit(t, submoduleDir, "config", "user.email", "test@test.com")
	mustRunGit(t, submoduleDir, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(submoduleDir, "README.md"), "# Test")
	mustRunGit(t, submoduleDir, "add", ".")
	mustRunGit(t, submoduleDir, "commit", "-m", "Initial commit")
	mustWriteFile(t, filepath.Join(submoduleDir, "more.txt"), "more content")
	mustRunGit(t, submoduleDir, "add", ".")
	mustRunGit(t, submoduleDir, "commit", "-m", "Second commit")

	// Create the parent repo
	mustRunGit(t, parentDir, "init")
	mustRunGit(t, parentDir, "config", "user.email", "test@test.com")
	mustRunGit(t, parentDir, "config", "user.name", "Test")

	// Add submodule (pointing to latest commit)
	mustRunGit(t, parentDir, "submodule", "add", submoduleDir, "projects/sub")
	mustRunGit(t, parentDir, "commit", "-m", "Add submodule")

	// Reset submodule to previous commit (now it's behind what parent expects)
	subPath := filepath.Join(parentDir, "projects/sub")
	mustRunGit(t, subPath, "checkout", "HEAD~1")

	// Run check - should detect submodule is behind
	check := NewCommitsCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false when submodule is behind")
	}
	if len(result.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(result.Issues))
	}

	// Verify issue details
	if len(result.Issues) > 0 {
		issue := result.Issues[0]
		if issue.Severity != doctor.SeverityWarning {
			t.Errorf("expected SeverityWarning, got %v", issue.Severity)
		}
		if !issue.AutoFixable {
			t.Error("expected AutoFixable to be true when submodule is behind")
		}
		if issue.Details["status"] != "behind" {
			t.Errorf("expected status 'behind', got %v", issue.Details["status"])
		}
	}
}

func TestCommitsCheck_Run_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := NewCommitsCheck()
	_, err := check.Run(ctx, t.TempDir())
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestCommitsCheck_Fix(t *testing.T) {
	// Create a parent repo with a submodule that is behind
	parentDir := t.TempDir()
	submoduleDir := t.TempDir()

	// Create the "remote" submodule repo
	mustRunGit(t, submoduleDir, "init")
	mustRunGit(t, submoduleDir, "config", "user.email", "test@test.com")
	mustRunGit(t, submoduleDir, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(submoduleDir, "README.md"), "# Test")
	mustRunGit(t, submoduleDir, "add", ".")
	mustRunGit(t, submoduleDir, "commit", "-m", "Initial commit")
	mustWriteFile(t, filepath.Join(submoduleDir, "more.txt"), "more content")
	mustRunGit(t, submoduleDir, "add", ".")
	mustRunGit(t, submoduleDir, "commit", "-m", "Second commit")

	// Create the parent repo
	mustRunGit(t, parentDir, "init")
	mustRunGit(t, parentDir, "config", "user.email", "test@test.com")
	mustRunGit(t, parentDir, "config", "user.name", "Test")

	// Add submodule
	mustRunGit(t, parentDir, "submodule", "add", submoduleDir, "projects/sub")
	mustRunGit(t, parentDir, "commit", "-m", "Add submodule")

	// Reset submodule to previous commit
	subPath := filepath.Join(parentDir, "projects/sub")
	mustRunGit(t, subPath, "checkout", "HEAD~1")

	// Run check to get the issue
	check := NewCommitsCheck()
	result, _ := check.Run(context.Background(), parentDir)
	if len(result.Issues) == 0 {
		t.Skip("no issues to fix")
	}

	// Fix the issues
	fixed, err := check.Fix(context.Background(), parentDir, result.Issues)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	if len(fixed) != 1 {
		t.Errorf("expected 1 fixed issue, got %d", len(fixed))
	}

	// Verify the fix worked
	result2, _ := check.Run(context.Background(), parentDir)
	if !result2.Passed {
		t.Error("expected Passed to be true after fix")
	}
}
