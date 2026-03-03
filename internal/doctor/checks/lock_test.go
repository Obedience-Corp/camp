package checks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/doctor"
)

func TestLockCheck_ID(t *testing.T) {
	check := NewLockCheck()
	if got := check.ID(); got != "lock" {
		t.Errorf("ID() = %q, want %q", got, "lock")
	}
}

func TestLockCheck_Name(t *testing.T) {
	check := NewLockCheck()
	if got := check.Name(); got == "" {
		t.Error("Name() returned empty string")
	}
}

func TestLockCheck_Description(t *testing.T) {
	check := NewLockCheck()
	if got := check.Description(); got == "" {
		t.Error("Description() returned empty string")
	}
}

func TestLockCheck_Run_NoLocks(t *testing.T) {
	repoDir := t.TempDir()
	mustRunGit(t, repoDir, "init")

	check := NewLockCheck()
	result, err := check.Run(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed to be true when no lock files exist")
	}
	if result.Total != 0 {
		t.Errorf("expected Total = 0, got %d", result.Total)
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %d", len(result.Issues))
	}
}

func TestLockCheck_Run_StaleLock(t *testing.T) {
	repoDir := t.TempDir()
	mustRunGit(t, repoDir, "init")

	// Create a synthetic stale lock file (no process holds it)
	lockPath := filepath.Join(repoDir, ".git", "index.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatalf("create lock file: %v", err)
	}

	check := NewLockCheck()
	result, err := check.Run(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("expected Passed to be false when stale lock exists")
	}
	if result.Total != 1 {
		t.Errorf("expected Total = 1, got %d", result.Total)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}

	issue := result.Issues[0]
	if issue.Severity != doctor.SeverityError {
		t.Errorf("expected SeverityError for stale lock, got %v", issue.Severity)
	}
	if issue.CheckID != "lock" {
		t.Errorf("expected CheckID 'lock', got %q", issue.CheckID)
	}
	if !issue.AutoFixable {
		t.Error("expected stale lock to be AutoFixable")
	}
	if issue.FixCommand == "" {
		t.Error("expected FixCommand to be set for stale lock")
	}
}

func TestLockCheck_Fix_RemovesStaleLock(t *testing.T) {
	repoDir := t.TempDir()
	mustRunGit(t, repoDir, "init")

	// Create a synthetic stale lock file
	lockPath := filepath.Join(repoDir, ".git", "index.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatalf("create lock file: %v", err)
	}

	check := NewLockCheck()

	// Run to get issues
	result, err := check.Run(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Issues) == 0 {
		t.Fatal("expected issues from Run before Fix")
	}

	// Fix should remove the stale lock
	fixed, err := check.Fix(context.Background(), repoDir, result.Issues)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	if len(fixed) != 1 {
		t.Errorf("expected 1 fixed issue, got %d", len(fixed))
	}

	// Verify lock file was removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed after Fix")
	}
}

func TestLockCheck_Fix_NoAutoFixableIssues(t *testing.T) {
	check := NewLockCheck()

	// Pass non-auto-fixable issues — should be a no-op
	issues := []doctor.Issue{
		{
			Severity:    doctor.SeverityWarning,
			CheckID:     "lock",
			AutoFixable: false,
			Details:     map[string]any{"path": "/tmp/fake.lock", "type": "active_lock"},
		},
	}

	fixed, err := check.Fix(context.Background(), t.TempDir(), issues)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}
	if len(fixed) != 0 {
		t.Errorf("expected 0 fixed issues for non-fixable input, got %d", len(fixed))
	}
}

func TestLockCheck_Run_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := NewLockCheck()
	_, err := check.Run(ctx, t.TempDir())
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}
