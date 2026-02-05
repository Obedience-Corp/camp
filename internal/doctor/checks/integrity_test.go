package checks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/doctor"
)

func TestIntegrityCheck_ID(t *testing.T) {
	check := NewIntegrityCheck()
	if got := check.ID(); got != "integrity" {
		t.Errorf("ID() = %q, want %q", got, "integrity")
	}
}

func TestIntegrityCheck_Name(t *testing.T) {
	check := NewIntegrityCheck()
	if got := check.Name(); got == "" {
		t.Error("Name() returned empty string")
	}
}

func TestIntegrityCheck_Description(t *testing.T) {
	check := NewIntegrityCheck()
	if got := check.Description(); got == "" {
		t.Error("Description() returned empty string")
	}
}

func TestIntegrityCheck_Run_NoSubmodules(t *testing.T) {
	// Create a temp git repo with no submodules
	repoDir := t.TempDir()
	mustRunGit(t, repoDir, "init")

	check := NewIntegrityCheck()
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

func TestIntegrityCheck_Run_InitializedSubmodule(t *testing.T) {
	// Create a parent repo with an initialized submodule
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

	// Run check - should pass
	check := NewIntegrityCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed to be true for initialized submodule")
	}
	if result.Total != 1 {
		t.Errorf("expected Total = 1, got %d", result.Total)
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %d", len(result.Issues))
	}
}

func TestIntegrityCheck_Run_MissingDirectory(t *testing.T) {
	// Create a parent repo with submodule in .gitmodules but missing directory
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

	// Remove the submodule directory (simulating a broken state)
	os.RemoveAll(filepath.Join(parentDir, "projects/sub"))

	// Run check - should detect missing directory
	check := NewIntegrityCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false for missing directory")
	}
	if len(result.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(result.Issues))
	}

	// Verify issue details
	if len(result.Issues) > 0 {
		issue := result.Issues[0]
		if issue.Severity != doctor.SeverityError {
			t.Errorf("expected SeverityError, got %v", issue.Severity)
		}
		if issue.CheckID != "integrity" {
			t.Errorf("expected CheckID 'integrity', got %q", issue.CheckID)
		}
		if !issue.AutoFixable {
			t.Error("expected AutoFixable to be true")
		}
	}
}

func TestIntegrityCheck_Run_MissingGitDir(t *testing.T) {
	// Create a parent repo with submodule directory but missing .git
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

	// Remove .git from submodule (simulating uninitialized state)
	os.Remove(filepath.Join(parentDir, "projects/sub/.git"))

	// Run check - should detect missing .git
	check := NewIntegrityCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false for missing .git")
	}
	if len(result.Issues) == 0 {
		t.Error("expected at least 1 issue")
	}
}

func TestIntegrityCheck_Run_EmptyDirectory(t *testing.T) {
	// Create a parent repo with empty submodule directory
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

	// Remove all content except .git (simulating empty checkout)
	subPath := filepath.Join(parentDir, "projects/sub")
	entries, _ := os.ReadDir(subPath)
	for _, entry := range entries {
		if entry.Name() != ".git" {
			os.RemoveAll(filepath.Join(subPath, entry.Name()))
		}
	}

	// Run check - should detect empty directory
	check := NewIntegrityCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false for empty directory")
	}
	if len(result.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(result.Issues))
	}

	// Verify it's a warning, not error
	if len(result.Issues) > 0 {
		issue := result.Issues[0]
		if issue.Severity != doctor.SeverityWarning {
			t.Errorf("expected SeverityWarning for empty dir, got %v", issue.Severity)
		}
	}
}

func TestIntegrityCheck_Run_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := NewIntegrityCheck()
	_, err := check.Run(ctx, t.TempDir())
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}
