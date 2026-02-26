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

func TestDetectDefaultBranchLocal_Main(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx := context.Background()
	branch := detectDefaultBranchLocal(ctx, dir)
	if branch != "main" {
		t.Fatalf("expected 'main', got %q", branch)
	}
}

func TestDetectDefaultBranchLocal_Master(t *testing.T) {
	dir := initBranchTestRepo(t, "master")

	ctx := context.Background()
	branch := detectDefaultBranchLocal(ctx, dir)
	if branch != "master" {
		t.Fatalf("expected 'master', got %q", branch)
	}
}

func TestDetectDefaultBranchLocal_Develop(t *testing.T) {
	dir := initBranchTestRepo(t, "develop")

	ctx := context.Background()
	branch := detectDefaultBranchLocal(ctx, dir)
	if branch != "develop" {
		t.Fatalf("expected 'develop', got %q", branch)
	}
}

func TestUnmergedBranchCount_InvalidPath(t *testing.T) {
	ctx := context.Background()
	count := UnmergedBranchCount(ctx, "/nonexistent/path")
	if count != 0 {
		t.Fatalf("expected 0 for invalid path, got %d", count)
	}
}
