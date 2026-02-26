package checks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/doctor"
)

// setupRepoWithOrphan creates a git repo with a declared submodule and an orphaned gitlink.
func setupRepoWithOrphan(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	exec.Command("git", "init", repoRoot).Run()
	exec.Command("git", "-C", repoRoot, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoRoot, "config", "user.name", "Test").Run()

	// Create a declared submodule
	subDir := filepath.Join(repoRoot, "projects/valid")
	os.MkdirAll(subDir, 0755)
	exec.Command("git", "init", subDir).Run()
	exec.Command("git", "-C", subDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", subDir, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(subDir, "README.md"), []byte("valid"), 0644)
	exec.Command("git", "-C", subDir, "add", ".").Run()
	exec.Command("git", "-C", subDir, "commit", "-m", "init").Run()

	// Add .gitmodules entry
	exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules",
		"submodule.projects/valid.path", "projects/valid").Run()
	exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules",
		"submodule.projects/valid.url", "https://example.com/valid.git").Run()

	// Add declared sub to index
	exec.Command("git", "-C", repoRoot, "add", "projects/valid").Run()

	// Create an orphaned gitlink
	orphanDir := filepath.Join(repoRoot, "projects/orphan")
	os.MkdirAll(orphanDir, 0755)
	exec.Command("git", "init", orphanDir).Run()
	exec.Command("git", "-C", orphanDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", orphanDir, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(orphanDir, "README.md"), []byte("orphan"), 0644)
	exec.Command("git", "-C", orphanDir, "add", ".").Run()
	exec.Command("git", "-C", orphanDir, "commit", "-m", "init").Run()

	// Add orphan to index (creates gitlink)
	exec.Command("git", "-C", repoRoot, "add", "projects/orphan").Run()

	// Commit everything while the orphan gitlink is still in the index.
	// Must commit BEFORE removing the directory, otherwise `git add .`
	// would detect the missing directory and unstage the gitlink.
	exec.Command("git", "-C", repoRoot, "add", ".").Run()
	exec.Command("git", "-C", repoRoot, "commit", "-m", "setup").Run()

	// Now remove the orphan directory; gitlink stays in committed index.
	os.RemoveAll(orphanDir)

	return repoRoot
}

func TestOrphanCheck_Run_DetectsOrphan(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupRepoWithOrphan(t)

	check := NewOrphanCheck()
	result, err := check.Run(ctx, repoRoot)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Passed {
		t.Error("Run() Passed = true, want false when orphan exists")
	}

	if len(result.Issues) != 1 {
		t.Fatalf("Run() found %d issues, want 1", len(result.Issues))
	}

	issue := result.Issues[0]
	if issue.Severity != doctor.SeverityError {
		t.Errorf("issue.Severity = %v, want SeverityError", issue.Severity)
	}
	if issue.Submodule != "projects/orphan" {
		t.Errorf("issue.Submodule = %q, want %q", issue.Submodule, "projects/orphan")
	}
	if !issue.AutoFixable {
		t.Error("issue.AutoFixable = false, want true")
	}
	if issue.CheckID != "orphan" {
		t.Errorf("issue.CheckID = %q, want %q", issue.CheckID, "orphan")
	}
}

func TestOrphanCheck_Run_NoOrphans(t *testing.T) {
	ctx := context.Background()

	// Create a clean repo with only declared submodules
	repoRoot := t.TempDir()
	exec.Command("git", "init", repoRoot).Run()
	exec.Command("git", "-C", repoRoot, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoRoot, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("clean"), 0644)
	exec.Command("git", "-C", repoRoot, "add", ".").Run()
	exec.Command("git", "-C", repoRoot, "commit", "-m", "init").Run()

	check := NewOrphanCheck()
	result, err := check.Run(ctx, repoRoot)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Passed {
		t.Error("Run() Passed = false, want true for clean repo")
	}
	if len(result.Issues) != 0 {
		t.Errorf("Run() found %d issues, want 0", len(result.Issues))
	}
}

func TestOrphanCheck_Fix(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupRepoWithOrphan(t)

	check := NewOrphanCheck()

	// Detect first
	result, err := check.Run(ctx, repoRoot)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}

	// Fix
	fixed, err := check.Fix(ctx, repoRoot, result.Issues)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}
	if len(fixed) != 1 {
		t.Fatalf("Fix() fixed %d issues, want 1", len(fixed))
	}

	// Verify fix
	result, err = check.Run(ctx, repoRoot)
	if err != nil {
		t.Fatalf("Run() after fix error = %v", err)
	}
	if !result.Passed {
		t.Error("Run() after fix: Passed = false, want true")
	}
}

func TestOrphanCheck_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	check := NewOrphanCheck()
	_, err := check.Run(ctx, t.TempDir())
	if err != context.Canceled {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestOrphanCheck_Metadata(t *testing.T) {
	check := NewOrphanCheck()

	if id := check.ID(); id != "orphan" {
		t.Errorf("ID() = %q, want %q", id, "orphan")
	}
	if name := check.Name(); name == "" {
		t.Error("Name() should not be empty")
	}
	if desc := check.Description(); desc == "" {
		t.Error("Description() should not be empty")
	}
}
