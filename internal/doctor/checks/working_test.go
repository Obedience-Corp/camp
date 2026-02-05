package checks

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/doctor"
)

func TestWorkingCheck_ID(t *testing.T) {
	check := NewWorkingCheck()
	if got := check.ID(); got != "working" {
		t.Errorf("ID() = %q, want %q", got, "working")
	}
}

func TestWorkingCheck_Name(t *testing.T) {
	check := NewWorkingCheck()
	if got := check.Name(); got == "" {
		t.Error("Name() returned empty string")
	}
}

func TestWorkingCheck_Description(t *testing.T) {
	check := NewWorkingCheck()
	if got := check.Description(); got == "" {
		t.Error("Description() returned empty string")
	}
}

func TestWorkingCheck_Run_NoSubmodules(t *testing.T) {
	// Create a temp git repo with no submodules
	repoDir := t.TempDir()
	mustRunGit(t, repoDir, "init")

	check := NewWorkingCheck()
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

func TestWorkingCheck_Run_CleanWorkingDirectory(t *testing.T) {
	// Create a parent repo with a clean submodule
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

	// Run check - should pass (clean working directory)
	check := NewWorkingCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed to be true for clean working directory")
	}
	if result.Total != 1 {
		t.Errorf("expected Total = 1, got %d", result.Total)
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %d", len(result.Issues))
	}
}

func TestWorkingCheck_Run_ModifiedFile(t *testing.T) {
	// Create a parent repo with a submodule that has modified file
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

	// Modify a file in the submodule
	subPath := filepath.Join(parentDir, "projects/sub")
	mustWriteFile(t, filepath.Join(subPath, "README.md"), "# Modified")

	// Run check - should detect modification
	check := NewWorkingCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false for modified file")
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
		if issue.CheckID != "working" {
			t.Errorf("expected CheckID 'working', got %q", issue.CheckID)
		}
		if issue.AutoFixable {
			t.Error("expected AutoFixable to be false")
		}
	}
}

func TestWorkingCheck_Run_UntrackedFile(t *testing.T) {
	// Create a parent repo with a submodule that has untracked file
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

	// Create an untracked file in the submodule
	subPath := filepath.Join(parentDir, "projects/sub")
	mustWriteFile(t, filepath.Join(subPath, "untracked.txt"), "new file")

	// Run check - should detect untracked file
	check := NewWorkingCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false for untracked file")
	}
	if len(result.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(result.Issues))
	}
}

func TestWorkingCheck_Run_StagedChanges(t *testing.T) {
	// Create a parent repo with a submodule that has staged changes
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

	// Create and stage a new file in the submodule
	subPath := filepath.Join(parentDir, "projects/sub")
	mustWriteFile(t, filepath.Join(subPath, "staged.txt"), "staged content")
	mustRunGit(t, subPath, "add", "staged.txt")

	// Run check - should detect staged changes
	check := NewWorkingCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false for staged changes")
	}
	if len(result.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(result.Issues))
	}
}

func TestWorkingCheck_Run_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := NewWorkingCheck()
	_, err := check.Run(ctx, t.TempDir())
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestWorkingCheck_Fix_NoOp(t *testing.T) {
	check := NewWorkingCheck()
	issues := []doctor.Issue{
		{Severity: doctor.SeverityWarning, CheckID: "working", Submodule: "test"},
	}

	fixed, err := check.Fix(context.Background(), t.TempDir(), issues)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	// Working check Fix should be a no-op
	if len(fixed) != 0 {
		t.Errorf("expected 0 fixed issues, got %d", len(fixed))
	}
}
