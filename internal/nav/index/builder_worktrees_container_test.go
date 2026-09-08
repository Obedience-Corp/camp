//go:build container_fs

package index

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/nav"
)

// These stage real repositories and git worktrees on disk, so they run inside
// the pooled container rather than on a developer's machine (decision D007).

// scanWorktrees runs one "git worktree list" per project concurrently. The
// hazard of a fan-out is a dropped result, so assert every project's worktrees
// survive the merge, including for a project that has several.
func TestScanWorktrees_IndexesEveryProjectsWorktrees(t *testing.T) {
	root := stageWorktreeCampaign(t, map[string][]string{
		"alpha": {"wt-a"},
		"beta":  {"wt-b1", "wt-b2"},
		"gamma": nil,
	})

	idx, err := NewBuilder(root).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	want := []string{"alpha@wt-a", "beta@wt-b1", "beta@wt-b2"}
	got := worktreeTargetNames(idx)

	if len(got) != len(want) {
		t.Fatalf("worktree targets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("worktree targets = %v, want %v", got, want)
		}
	}
}

// The index is cached and compared as a whole, and a human reads it when a
// jump goes somewhere unexpected. Concurrency must not leave the order to
// whichever "git worktree list" finished first.
func TestScanWorktrees_OrderIsStableAcrossBuilds(t *testing.T) {
	root := stageWorktreeCampaign(t, map[string][]string{
		"alpha":   {"wt-a1", "wt-a2"},
		"beta":    {"wt-b"},
		"gamma":   {"wt-g1", "wt-g2", "wt-g3"},
		"delta":   {"wt-d"},
		"epsilon": {"wt-e1", "wt-e2"},
	})
	ctx := context.Background()

	first, err := NewBuilder(root).Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	baseline := worktreeTargetNames(first)
	if len(baseline) != 9 {
		t.Fatalf("first build found %d worktree targets %v, want 9", len(baseline), baseline)
	}

	for attempt := 2; attempt <= 6; attempt++ {
		idx, err := NewBuilder(root).Build(ctx)
		if err != nil {
			t.Fatalf("Build attempt %d: %v", attempt, err)
		}
		got := worktreeTargetNames(idx)
		if len(got) != len(baseline) {
			t.Fatalf("build %d found %v, first build found %v", attempt, got, baseline)
		}
		for i := range baseline {
			if got[i] != baseline[i] {
				t.Fatalf("build %d ordered targets %v, first build gave %v", attempt, got, baseline)
			}
		}
	}
}

// worktreeTargetNames returns the indexed worktree target names in index order.
func worktreeTargetNames(idx *Index) []string {
	var names []string
	for _, target := range idx.Targets {
		if target.Category == nav.CategoryWorktrees {
			names = append(names, target.Name)
		}
	}
	return names
}

// stageWorktreeCampaign builds a campaign whose projects/ holds one committed
// git repo per key, each with a linked worktree per listed name under the
// conventional projects/worktrees/<project>/ layout.
func stageWorktreeCampaign(t *testing.T, layout map[string][]string) string {
	t.Helper()

	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	for project, worktrees := range layout {
		projectPath := filepath.Join(root, "projects", project)
		if err := os.MkdirAll(projectPath, 0o755); err != nil {
			t.Fatal(err)
		}
		initCommittedRepo(t, projectPath)

		for _, name := range worktrees {
			path := filepath.Join(root, "projects", "worktrees", project, name)
			runGitForTest(t, projectPath, "worktree", "add", path)
		}
	}
	return root
}

// initCommittedRepo makes path a git repository with one commit, which git
// worktree add requires.
func initCommittedRepo(t *testing.T, path string) {
	t.Helper()

	runGitForTest(t, "", "init", path)
	runGitForTest(t, path, "config", "user.email", "test@test.com")
	runGitForTest(t, path, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, path, "add", ".")
	runGitForTest(t, path, "commit", "-m", "fixture")
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %q: %v: %s", args, dir, err, out)
	}
}
