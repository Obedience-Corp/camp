package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initBranchTestRepo creates a temp git repo with an initial commit on the given branch.
func initBranchTestRepo(t *testing.T, defaultBranch string) string {
	t.Helper()

	dir := t.TempDir()

	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", defaultBranch)
	// Create a file so we have something to commit
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")

	return dir
}

func TestUnmergedBranchCount_NoUnmerged(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx := context.Background()
	count := UnmergedBranchCount(ctx, dir)
	if count != 0 {
		t.Fatalf("expected 0 unmerged branches, got %d", count)
	}
}

func TestUnmergedBranchCount_WithUnmerged(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Create an unmerged branch with a unique commit
	run("checkout", "-b", "feature-a")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature a")

	// Create another unmerged branch
	run("checkout", "main")
	run("checkout", "-b", "feature-b")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature b")

	// Go back to main
	run("checkout", "main")

	ctx := context.Background()
	count := UnmergedBranchCount(ctx, dir)
	if count != 2 {
		t.Fatalf("expected 2 unmerged branches, got %d", count)
	}
}

func TestUnmergedBranchCount_MergedBranchNotCounted(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Create a branch and merge it
	run("checkout", "-b", "feature-merged")
	if err := os.WriteFile(filepath.Join(dir, "merged.txt"), []byte("merged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature merged")
	run("checkout", "main")
	run("merge", "feature-merged")

	// Create an unmerged branch
	run("checkout", "-b", "feature-unmerged")
	if err := os.WriteFile(filepath.Join(dir, "unmerged.txt"), []byte("unmerged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature unmerged")
	run("checkout", "main")

	ctx := context.Background()
	count := UnmergedBranchCount(ctx, dir)
	if count != 1 {
		t.Fatalf("expected 1 unmerged branch, got %d", count)
	}
}

func TestUnmergedBranchCount_ContextCancelled(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	count := UnmergedBranchCount(ctx, dir)
	if count != 0 {
		t.Fatalf("expected 0 for cancelled context, got %d", count)
	}
}

func TestDefaultBranch_Main(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx := context.Background()
	branch := DefaultBranch(ctx, dir)
	if branch != "main" {
		t.Fatalf("expected 'main', got %q", branch)
	}
}

func TestDefaultBranch_Master(t *testing.T) {
	dir := initBranchTestRepo(t, "master")

	ctx := context.Background()
	branch := DefaultBranch(ctx, dir)
	if branch != "master" {
		t.Fatalf("expected 'master', got %q", branch)
	}
}

func TestDefaultBranch_Develop_NotRecognized(t *testing.T) {
	dir := initBranchTestRepo(t, "develop")

	ctx := context.Background()
	branch := DefaultBranch(ctx, dir)
	// develop is no longer in the fallback list — should return empty
	if branch != "" {
		t.Fatalf("expected empty string (develop not in fallback), got %q", branch)
	}
}

func TestUnmergedBranchCount_InvalidPath(t *testing.T) {
	ctx := context.Background()
	count := UnmergedBranchCount(ctx, "/nonexistent/path")
	if count != 0 {
		t.Fatalf("expected 0 for invalid path, got %d", count)
	}
}

// gitRunner returns a helper that runs git commands in a directory with test env.
func gitRunner(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	return func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx := context.Background()
	branch := CurrentBranch(ctx, dir)
	if branch != "main" {
		t.Fatalf("expected 'main', got %q", branch)
	}
}

func TestCurrentBranch_FeatureBranch(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	run := gitRunner(t, dir)

	run("checkout", "-b", "feature-x")

	ctx := context.Background()
	branch := CurrentBranch(ctx, dir)
	if branch != "feature-x" {
		t.Fatalf("expected 'feature-x', got %q", branch)
	}
}

func TestCurrentBranch_CancelledContext(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	branch := CurrentBranch(ctx, dir)
	if branch != "" {
		t.Fatalf("expected empty for cancelled context, got %q", branch)
	}
}

func TestMergedBranches_NoMerged(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx := context.Background()
	branches, err := MergedBranches(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 0 {
		t.Fatalf("expected 0 merged branches, got %d: %v", len(branches), branches)
	}
}

func TestMergedBranches_ReturnsMerged(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	run := gitRunner(t, dir)

	// Create and merge a branch
	run("checkout", "-b", "feature-done")
	if err := os.WriteFile(filepath.Join(dir, "done.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature done")
	run("checkout", "main")
	run("merge", "feature-done")

	ctx := context.Background()
	branches, err := MergedBranches(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 {
		t.Fatalf("expected 1 merged branch, got %d: %v", len(branches), branches)
	}
	if branches[0] != "feature-done" {
		t.Fatalf("expected 'feature-done', got %q", branches[0])
	}
}

func TestMergedBranches_ExcludesDefaultAndCurrent(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	run := gitRunner(t, dir)

	// Create and merge two branches
	run("checkout", "-b", "feat-a")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feat a")
	run("checkout", "main")
	run("merge", "feat-a")

	run("checkout", "-b", "feat-b")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feat b")
	run("checkout", "main")
	run("merge", "feat-b")

	// Stay on main — both feat-a and feat-b should be returned, not main
	ctx := context.Background()
	branches, err := MergedBranches(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 {
		t.Fatalf("expected 2 merged branches, got %d: %v", len(branches), branches)
	}
	for _, b := range branches {
		if b == "main" {
			t.Fatal("default branch 'main' should be excluded from merged list")
		}
	}
}

func TestDeleteBranch(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	run := gitRunner(t, dir)

	// Create and merge a branch
	run("checkout", "-b", "to-delete")
	if err := os.WriteFile(filepath.Join(dir, "del.txt"), []byte("del\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "to delete")
	run("checkout", "main")
	run("merge", "to-delete")

	ctx := context.Background()
	if err := DeleteBranch(ctx, dir, "to-delete"); err != nil {
		t.Fatalf("failed to delete branch: %v", err)
	}

	// Verify it's gone
	branches, _ := MergedBranches(ctx, dir)
	for _, b := range branches {
		if b == "to-delete" {
			t.Fatal("branch 'to-delete' should have been deleted")
		}
	}
}

func TestDeleteBranch_UnmergedFails(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	run := gitRunner(t, dir)

	// Create an unmerged branch
	run("checkout", "-b", "unmerged-branch")
	if err := os.WriteFile(filepath.Join(dir, "um.txt"), []byte("um\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "unmerged work")
	run("checkout", "main")

	ctx := context.Background()
	err := DeleteBranch(ctx, dir, "unmerged-branch")
	if err == nil {
		t.Fatal("expected error deleting unmerged branch with -d, got nil")
	}
}

func TestDeleteBranch_CancelledContext(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := DeleteBranch(ctx, dir, "any-branch")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestMergedBranches_CancelledContext(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	branches, err := MergedBranches(ctx, dir)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if branches != nil {
		t.Fatalf("expected nil branches for cancelled context, got %v", branches)
	}
}
