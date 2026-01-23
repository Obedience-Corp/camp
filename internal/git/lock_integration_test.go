//go:build integration

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestLockHandling_RealGitRepo tests lock handling in a real git repository.
func TestLockHandling_RealGitRepo(t *testing.T) {
	// Create a real git repository
	tmpDir := t.TempDir()
	cmd := exec.Command("git", "init", tmpDir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	// Create a stale lock
	gitDir := filepath.Join(tmpDir, ".git")
	lockPath := filepath.Join(gitDir, "index.lock")
	os.WriteFile(lockPath, []byte{}, 0644)

	// Test full cleanup flow
	ctx := context.Background()
	result, err := CleanStaleLocks(ctx, tmpDir, nil)
	if err != nil {
		t.Fatalf("CleanStaleLocks() error = %v", err)
	}

	if len(result.Removed) != 1 {
		t.Errorf("Removed %d locks, want 1", len(result.Removed))
	}

	// Verify git operations work now
	cmd = exec.Command("git", "-C", tmpDir, "status")
	if err := cmd.Run(); err != nil {
		t.Errorf("git status failed after lock cleanup: %v", err)
	}
}

// TestLockHandling_RealSubmodule tests lock handling with a real submodule structure.
func TestLockHandling_RealSubmodule(t *testing.T) {
	// Create main repo
	mainDir := t.TempDir()
	cmd := exec.Command("git", "init", mainDir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	// Create initial commit
	readmePath := filepath.Join(mainDir, "README.md")
	os.WriteFile(readmePath, []byte("# Main"), 0644)
	exec.Command("git", "-C", mainDir, "add", ".").Run()
	exec.Command("git", "-C", mainDir, "commit", "-m", "init").Run()

	// Create submodule repo
	subDir := t.TempDir()
	exec.Command("git", "init", subDir).Run()
	subReadme := filepath.Join(subDir, "README.md")
	os.WriteFile(subReadme, []byte("# Sub"), 0644)
	exec.Command("git", "-C", subDir, "add", ".").Run()
	exec.Command("git", "-C", subDir, "commit", "-m", "init").Run()

	// Add as submodule (this creates .git/modules/)
	exec.Command("git", "-C", mainDir, "submodule", "add", subDir, "sub").Run()

	// Create a lock in the submodule's git directory
	modulesLock := filepath.Join(mainDir, ".git", "modules", "sub", "index.lock")
	os.MkdirAll(filepath.Dir(modulesLock), 0755)
	os.WriteFile(modulesLock, []byte{}, 0644)

	// Find locks
	ctx := context.Background()
	gitDir := filepath.Join(mainDir, ".git")
	locks, err := FindIndexLocks(ctx, gitDir)
	if err != nil {
		t.Fatalf("FindIndexLocks() error = %v", err)
	}

	// Should find the submodule lock
	if len(locks) == 0 {
		t.Error("FindIndexLocks() found no locks, expected to find submodule lock")
	}

	// Clean up
	result, err := CleanStaleLocks(ctx, mainDir, nil)
	if err != nil {
		t.Fatalf("CleanStaleLocks() error = %v", err)
	}

	if !result.AllRemoved() {
		t.Errorf("Not all locks removed: %s", result.Summary())
	}
}
