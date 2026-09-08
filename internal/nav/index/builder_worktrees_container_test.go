//go:build container_fs

package index

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/project"
)

// These stage real repositories and worktrees, so they run in the pooled
// container (D007).

// The hazard of a fan-out is a dropped result, so assert every project's
// worktrees survive the merge, including a project with several.
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

// Concurrency must not leave the order to whichever subprocess finished first.
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

// stageWorktreeCampaign gives projects/ one committed repo per key, each with a
// linked worktree per listed name.
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

// initCommittedRepo makes path a repo with the one commit worktree add needs.
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
	runGitForTestEnv(t, dir, nil, args...)
}

func runGitForTestEnv(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %q: %v: %s", args, dir, err, out)
	}
}

// Two checkouts of one remote each keep their worktrees.
//
// project.List drops the older checkout, so the index used to hide one
// project's worktrees outright, and which one it hid flipped with every commit
// to the other. The projects category never deduped, so "cgo p camp" resolved
// while "camp@wt-a" did not exist. This makes worktrees agree with projects;
// names stay distinct because the project directory name is the prefix.
func TestScanWorktrees_KeepsWorktreesOfEveryCheckoutOfARemote(t *testing.T) {
	const remote = "git@github.com:Obedience-Corp/camp.git"
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	stageCheckoutOfRemote(t, root, "camp", remote, "2026-01-01T00:00:00", "wt-a")
	stageCheckoutOfRemote(t, root, "camp-copy", remote, "2026-06-01T00:00:00", "wt-b")

	ctx := context.Background()

	// The dedup that hid a checkout is still real: assert it, do not describe it.
	canonical, err := project.List(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 1 {
		t.Fatalf("project.List returned %d checkouts, want 1 canonical", len(canonical))
	}

	idx, err := NewBuilder(root).Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	want := []string{"camp@wt-a", "camp-copy@wt-b"}
	got := worktreeTargetNames(idx)
	if len(got) != len(want) {
		t.Fatalf("worktree targets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("worktree targets = %v, want %v", got, want)
		}
	}

	// Both checkouts were always navigable as projects.
	for _, name := range []string{"camp", "camp-copy"} {
		if idx.Find(name) == nil {
			t.Fatalf("project target %q missing; the projects category does not dedup", name)
		}
	}
}

// stageCheckoutOfRemote makes projects/<name> a checkout of remote committed at
// date, with one linked worktree.
func stageCheckoutOfRemote(t *testing.T, root, name, remote, date, worktreeName string) {
	t.Helper()

	path := filepath.Join(root, "projects", name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, "", "init", path)
	runGitForTest(t, path, "remote", "add", "origin", remote)
	runGitForTest(t, path, "config", "user.email", "test@test.com")
	runGitForTest(t, path, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, path, "add", ".")
	runGitForTestEnv(t, path, []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date},
		"commit", "-m", name)
	runGitForTest(t, path, "worktree", "add", filepath.Join(root, "projects", "worktrees", name, worktreeName))
}
