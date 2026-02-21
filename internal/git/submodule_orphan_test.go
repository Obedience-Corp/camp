package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupGitRepoWithSubmodule creates a git repo with a .gitmodules file declaring
// the given submodule path, and optionally adds an orphaned gitlink to the index.
func setupGitRepoWithSubmodule(t *testing.T, subPath, orphanPath string) string {
	t.Helper()

	repoRoot := setupGitRepo(t)

	// Create .gitmodules declaring one submodule
	if subPath != "" {
		cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules",
			"submodule."+subPath+".path", subPath)
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to set .gitmodules path: %v", err)
		}
		cmd = exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules",
			"submodule."+subPath+".url", "https://example.com/"+subPath+".git")
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to set .gitmodules url: %v", err)
		}

		// Create a real submodule-like entry: init a sub-repo, add it to the index
		subDir := filepath.Join(repoRoot, subPath)
		os.MkdirAll(subDir, 0755)
		exec.Command("git", "init", subDir).Run()
		exec.Command("git", "-C", subDir, "config", "user.email", "test@test.com").Run()
		exec.Command("git", "-C", subDir, "config", "user.name", "Test").Run()
		os.WriteFile(filepath.Join(subDir, "README.md"), []byte("sub"), 0644)
		exec.Command("git", "-C", subDir, "add", ".").Run()
		exec.Command("git", "-C", subDir, "commit", "-m", "init").Run()

		// Add to parent index as a gitlink
		exec.Command("git", "-C", repoRoot, "add", subPath).Run()
	}

	// Add an orphaned gitlink (in index but not in .gitmodules)
	if orphanPath != "" {
		orphanDir := filepath.Join(repoRoot, orphanPath)
		os.MkdirAll(orphanDir, 0755)
		exec.Command("git", "init", orphanDir).Run()
		exec.Command("git", "-C", orphanDir, "config", "user.email", "test@test.com").Run()
		exec.Command("git", "-C", orphanDir, "config", "user.name", "Test").Run()
		os.WriteFile(filepath.Join(orphanDir, "README.md"), []byte("orphan"), 0644)
		exec.Command("git", "-C", orphanDir, "add", ".").Run()
		exec.Command("git", "-C", orphanDir, "commit", "-m", "init").Run()

		// Add to parent index (creates a gitlink)
		exec.Command("git", "-C", repoRoot, "add", orphanPath).Run()
	}

	// Commit everything while the orphan gitlink is still in the index.
	// We must commit BEFORE removing the orphan directory, otherwise
	// `git add .` would detect the missing directory and unstage the gitlink.
	exec.Command("git", "-C", repoRoot, "add", ".").Run()
	exec.Command("git", "-C", repoRoot, "commit", "-m", "setup").Run()

	// Now remove the orphan directory. The gitlink stays in the committed
	// index even though the directory is gone (simulating the real-world
	// scenario where a submodule was removed from .gitmodules but the
	// gitlink was never cleaned from the index).
	if orphanPath != "" {
		orphanDir := filepath.Join(repoRoot, orphanPath)
		os.RemoveAll(orphanDir)
	}

	return repoRoot
}

func TestListOrphanedGitlinks_NoOrphans(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepoWithSubmodule(t, "projects/valid", "")

	orphans, err := ListOrphanedGitlinks(ctx, repoRoot)
	if err != nil {
		t.Fatalf("ListOrphanedGitlinks() error = %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("ListOrphanedGitlinks() = %d orphans, want 0", len(orphans))
	}
}

func TestListOrphanedGitlinks_WithOrphan(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepoWithSubmodule(t, "projects/valid", "projects/orphan")

	orphans, err := ListOrphanedGitlinks(ctx, repoRoot)
	if err != nil {
		t.Fatalf("ListOrphanedGitlinks() error = %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("ListOrphanedGitlinks() = %d orphans, want 1", len(orphans))
	}
	if orphans[0].Path != "projects/orphan" {
		t.Errorf("orphan.Path = %q, want %q", orphans[0].Path, "projects/orphan")
	}
	if orphans[0].Commit == "" {
		t.Error("orphan.Commit should not be empty")
	}
}

func TestListOrphanedGitlinks_EmptyRepo(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)

	// Create an empty initial commit so git ls-files works
	os.WriteFile(filepath.Join(repoRoot, ".gitkeep"), []byte(""), 0644)
	exec.Command("git", "-C", repoRoot, "add", ".").Run()
	exec.Command("git", "-C", repoRoot, "commit", "-m", "init").Run()

	orphans, err := ListOrphanedGitlinks(ctx, repoRoot)
	if err != nil {
		t.Fatalf("ListOrphanedGitlinks() error = %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("ListOrphanedGitlinks() = %d orphans, want 0", len(orphans))
	}
}

func TestListOrphanedGitlinks_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupGitRepo(t)

	_, err := ListOrphanedGitlinks(ctx, repoRoot)
	if err != context.Canceled {
		t.Errorf("ListOrphanedGitlinks() error = %v, want context.Canceled", err)
	}
}

func TestRemoveOrphanedGitlinks(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepoWithSubmodule(t, "projects/valid", "projects/orphan")

	// Verify orphan exists
	orphans, err := ListOrphanedGitlinks(ctx, repoRoot)
	if err != nil {
		t.Fatalf("ListOrphanedGitlinks() error = %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(orphans))
	}

	// Remove orphans
	removed, err := RemoveOrphanedGitlinks(ctx, repoRoot, orphans)
	if err != nil {
		t.Fatalf("RemoveOrphanedGitlinks() error = %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("RemoveOrphanedGitlinks() removed %d, want 1", len(removed))
	}
	if removed[0] != "projects/orphan" {
		t.Errorf("removed[0] = %q, want %q", removed[0], "projects/orphan")
	}

	// Verify orphan is gone
	orphans, err = ListOrphanedGitlinks(ctx, repoRoot)
	if err != nil {
		t.Fatalf("ListOrphanedGitlinks() after removal error = %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("expected 0 orphans after removal, got %d", len(orphans))
	}
}

func TestRemoveOrphanedGitlinks_Empty(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)

	removed, err := RemoveOrphanedGitlinks(ctx, repoRoot, nil)
	if err != nil {
		t.Fatalf("RemoveOrphanedGitlinks() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("RemoveOrphanedGitlinks() removed %d, want 0", len(removed))
	}
}

func TestRemoveOrphanedGitlinks_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupGitRepo(t)
	orphans := []OrphanedGitlink{{Path: "projects/orphan", Commit: "abc123"}}

	_, err := RemoveOrphanedGitlinks(ctx, repoRoot, orphans)
	if err != context.Canceled {
		t.Errorf("RemoveOrphanedGitlinks() error = %v, want context.Canceled", err)
	}
}

func TestListIndexGitlinks(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepoWithSubmodule(t, "projects/sub", "")

	links, err := listIndexGitlinks(ctx, repoRoot)
	if err != nil {
		t.Fatalf("listIndexGitlinks() error = %v", err)
	}

	// Should find the submodule gitlink
	found := false
	for _, link := range links {
		if link.Path == "projects/sub" {
			found = true
			if link.Commit == "" {
				t.Error("gitlink.Commit should not be empty")
			}
		}
	}
	if !found {
		t.Errorf("listIndexGitlinks() did not find projects/sub, got %v", links)
	}
}
