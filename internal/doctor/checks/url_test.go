package checks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/doctor"
)

func TestURLCheck_ID(t *testing.T) {
	check := NewURLCheck()
	if got := check.ID(); got != "url" {
		t.Errorf("ID() = %q, want %q", got, "url")
	}
}

func TestURLCheck_Name(t *testing.T) {
	check := NewURLCheck()
	if got := check.Name(); got == "" {
		t.Error("Name() returned empty string")
	}
}

func TestURLCheck_Description(t *testing.T) {
	check := NewURLCheck()
	if got := check.Description(); got == "" {
		t.Error("Description() returned empty string")
	}
}

func TestURLCheck_Run_NoSubmodules(t *testing.T) {
	// Create a temp git repo with no submodules
	repoDir := t.TempDir()
	mustRunGit(t, repoDir, "init")

	check := NewURLCheck()
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

func TestURLCheck_Run_MatchingURLs(t *testing.T) {
	// Create a parent repo with a submodule
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

	// Run check - URLs should match
	check := NewURLCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed to be true when URLs match")
	}
	if result.Total != 1 {
		t.Errorf("expected Total = 1, got %d", result.Total)
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %d", len(result.Issues))
	}
}

func TestURLCheck_Run_MismatchedURLs(t *testing.T) {
	// Create a parent repo with a submodule
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

	// Manually change .gitmodules URL without running sync
	gitmodulesPath := filepath.Join(parentDir, ".gitmodules")
	content, _ := os.ReadFile(gitmodulesPath)
	newContent := string(content)
	newContent = newContent[:len(newContent)-1] + "-changed\n"
	mustWriteFile(t, gitmodulesPath, newContent)

	// Run check - URLs should NOT match
	check := NewURLCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false when URLs mismatch")
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
		if issue.CheckID != "url" {
			t.Errorf("expected CheckID 'url', got %q", issue.CheckID)
		}
		if !issue.AutoFixable {
			t.Error("expected AutoFixable to be true")
		}
		if issue.FixCommand != "git submodule sync --recursive" {
			t.Errorf("unexpected FixCommand: %q", issue.FixCommand)
		}
	}
}

func TestURLCheck_Run_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := NewURLCheck()
	_, err := check.Run(ctx, t.TempDir())
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestURLCheck_Fix(t *testing.T) {
	// Create a parent repo with a submodule
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

	// Manually change .gitmodules URL without running sync
	gitmodulesPath := filepath.Join(parentDir, ".gitmodules")
	content, _ := os.ReadFile(gitmodulesPath)
	newContent := string(content)
	newContent = newContent[:len(newContent)-1] + "-changed\n"
	mustWriteFile(t, gitmodulesPath, newContent)

	// Run check first to verify mismatch
	check := NewURLCheck()
	result, _ := check.Run(context.Background(), parentDir)
	if len(result.Issues) == 0 {
		t.Skip("no mismatch detected, cannot test fix")
	}

	// Fix the issues
	fixed, err := check.Fix(context.Background(), parentDir, result.Issues)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	if len(fixed) != len(result.Issues) {
		t.Errorf("expected %d fixed issues, got %d", len(result.Issues), len(fixed))
	}

	// Verify the fix worked
	result2, _ := check.Run(context.Background(), parentDir)
	if !result2.Passed {
		t.Error("expected Passed to be true after fix")
	}
}

// Helper functions

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Allow file:// protocol for submodule clones in tests
	cmd.Env = append(os.Environ(), "GIT_ALLOW_PROTOCOL=file")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
