package checks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obediencecorp/camp/internal/doctor"
)

func TestHeadCheck_ID(t *testing.T) {
	check := NewHeadCheck()
	if got := check.ID(); got != "head" {
		t.Errorf("ID() = %q, want %q", got, "head")
	}
}

func TestHeadCheck_Name(t *testing.T) {
	check := NewHeadCheck()
	if got := check.Name(); got == "" {
		t.Error("Name() returned empty string")
	}
}

func TestHeadCheck_Description(t *testing.T) {
	check := NewHeadCheck()
	if got := check.Description(); got == "" {
		t.Error("Description() returned empty string")
	}
}

func TestHeadCheck_Run_NoSubmodules(t *testing.T) {
	// Create a temp git repo with no submodules
	repoDir := t.TempDir()
	mustRunGit(t, repoDir, "init")

	check := NewHeadCheck()
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

func TestHeadCheck_Run_AttachedHead(t *testing.T) {
	// Create a parent repo with a submodule on a branch
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

	// Checkout the existing default branch in the submodule (attached HEAD)
	subPath := filepath.Join(parentDir, "projects/sub")
	mustRunGit(t, subPath, "checkout", "-b", "develop")

	// Run check - attached HEAD should have no issues
	check := NewHeadCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed to be true for attached HEAD")
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues for attached HEAD, got %d", len(result.Issues))
	}
}

func TestHeadCheck_Run_DetachedHeadSafe(t *testing.T) {
	// Create a parent repo with a submodule in detached HEAD (no local commits)
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

	// Add submodule - this puts it in detached HEAD by default
	mustRunGit(t, parentDir, "submodule", "add", submoduleDir, "projects/sub")
	mustRunGit(t, parentDir, "commit", "-m", "Add submodule")

	// Submodule should be in detached HEAD with no local commits - safe
	check := NewHeadCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// No issues expected since detached HEAD with no local commits is safe
	if !result.Passed {
		t.Error("expected Passed to be true for safe detached HEAD")
	}
}

func TestHeadCheck_Run_DetachedHeadWithCommits(t *testing.T) {
	// Create a parent repo with a submodule that has unpushed commits
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

	// Create a local commit in the submodule (while in detached HEAD)
	subPath := filepath.Join(parentDir, "projects/sub")
	mustRunGit(t, subPath, "config", "user.email", "test@test.com")
	mustRunGit(t, subPath, "config", "user.name", "Test")

	// First, explicitly checkout HEAD detached (checkout the current commit by hash)
	mustRunGit(t, subPath, "checkout", "--detach", "HEAD")

	// Delete all local branches and remotes to ensure our commit is truly orphaned
	deleteBranches(t, subPath)
	// Remove origin remote so commits aren't reachable from remote tracking branches
	mustRunGit(t, subPath, "remote", "remove", "origin")

	// Now create a local commit (we're in detached HEAD now)
	mustWriteFile(t, filepath.Join(subPath, "new_file.txt"), "local change")
	mustRunGit(t, subPath, "add", ".")
	mustRunGit(t, subPath, "commit", "-m", "Local commit that could be lost")

	// Run check - should detect warning
	check := NewHeadCheck()
	result, err := check.Run(context.Background(), parentDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false for detached HEAD with local commits")
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
		if issue.CheckID != "head" {
			t.Errorf("expected CheckID 'head', got %q", issue.CheckID)
		}
		if issue.AutoFixable {
			t.Error("expected AutoFixable to be false")
		}
		if issue.Details == nil {
			t.Error("expected Details to be set")
		}
	}
}

func TestHeadCheck_Run_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := NewHeadCheck()
	_, err := check.Run(ctx, t.TempDir())
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestHeadCheck_Fix_NoOp(t *testing.T) {
	check := NewHeadCheck()
	issues := []doctor.Issue{
		{Severity: doctor.SeverityWarning, CheckID: "head", Submodule: "test"},
	}

	fixed, err := check.Fix(context.Background(), t.TempDir(), issues)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	// HEAD check Fix should be a no-op
	if len(fixed) != 0 {
		t.Errorf("expected 0 fixed issues, got %d", len(fixed))
	}
}

func deleteBranches(t *testing.T, dir string) {
	t.Helper()
	// Get list of all local branches
	cmd := exec.Command("git", "branch", "--format=%(refname:short)")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_ALLOW_PROTOCOL=file")
	output, err := cmd.Output()
	if err != nil {
		return // No branches to delete
	}

	branches := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" || strings.HasPrefix(branch, "*") {
			continue
		}
		// Try to delete the branch (ignore errors if it fails)
		cmd := exec.Command("git", "branch", "-D", branch)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_ALLOW_PROTOCOL=file")
		_ = cmd.Run()
	}
}
