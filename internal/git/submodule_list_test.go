package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// setupRepoWithGitmodules creates a git repo with a .gitmodules file
// listing the given submodule paths.
func setupRepoWithGitmodules(t *testing.T, subPaths []string) string {
	t.Helper()
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()

	for _, p := range subPaths {
		name := filepath.Base(p)
		exec.Command("git", "-C", dir, "config", "-f", ".gitmodules",
			"submodule."+name+".path", p).Run()
		exec.Command("git", "-C", dir, "config", "-f", ".gitmodules",
			"submodule."+name+".url", "https://example.com/"+name+".git").Run()

		// Create the directory so it looks like a real submodule.
		os.MkdirAll(filepath.Join(dir, p), 0o755)
	}

	return dir
}

func TestListSubmodulePathsRecursive(t *testing.T) {
	ctx := context.Background()

	// Create a root repo with two top-level submodules.
	root := setupRepoWithGitmodules(t, []string{
		"projects/solo-project",
		"projects/monorepo",
	})

	// Give the monorepo submodule its own .gitmodules with nested children.
	monorepoDir := filepath.Join(root, "projects", "monorepo")
	exec.Command("git", "init", monorepoDir).Run()
	for _, child := range []string{"child-a", "child-b"} {
		exec.Command("git", "-C", monorepoDir, "config", "-f", ".gitmodules",
			"submodule."+child+".path", child).Run()
		exec.Command("git", "-C", monorepoDir, "config", "-f", ".gitmodules",
			"submodule."+child+".url", "https://example.com/"+child+".git").Run()
		os.MkdirAll(filepath.Join(monorepoDir, child), 0o755)
	}

	paths, err := ListSubmodulePathsRecursive(ctx, root, "projects/")
	if err != nil {
		t.Fatalf("ListSubmodulePathsRecursive() error = %v", err)
	}

	sort.Strings(paths)
	want := []string{
		"projects/monorepo",
		"projects/monorepo/child-a",
		"projects/monorepo/child-b",
		"projects/solo-project",
	}
	sort.Strings(want)

	if len(paths) != len(want) {
		t.Fatalf("got %d paths, want %d: %v", len(paths), len(want), paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestListSubmodulePathsRecursive_NoNested(t *testing.T) {
	ctx := context.Background()

	// Create a root repo with two plain submodules (no nested .gitmodules).
	root := setupRepoWithGitmodules(t, []string{
		"projects/alpha",
		"projects/beta",
	})

	paths, err := ListSubmodulePathsRecursive(ctx, root, "projects/")
	if err != nil {
		t.Fatalf("ListSubmodulePathsRecursive() error = %v", err)
	}

	sort.Strings(paths)
	want := []string{"projects/alpha", "projects/beta"}

	if len(paths) != len(want) {
		t.Fatalf("got %d paths, want %d: %v", len(paths), len(want), paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestListSubmodulePathsRecursive_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ListSubmodulePathsRecursive(ctx, t.TempDir(), "projects/")
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestSubmoduleDisplayName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"projects/camp", "camp"},
		{"projects/obey-platform-monorepo/obey", "obey-platform-monorepo/obey"},
		{"projects/obey-platform-monorepo/fest", "obey-platform-monorepo/fest"},
		{"projects/festival/camp", "festival/camp"},
		{"single", "single"},
		{"a/b", "b"},
		{"a/b/c/d", "c/d"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := SubmoduleDisplayName(tt.path)
			if got != tt.want {
				t.Errorf("SubmoduleDisplayName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
