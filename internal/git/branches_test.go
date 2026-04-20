package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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

func TestIsAncestor(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	run := gitRunner(t, dir)

	baseCommit := strings.TrimSpace(runOutput(t, dir, "rev-parse", "HEAD"))

	run("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature")
	featureCommit := strings.TrimSpace(runOutput(t, dir, "rev-parse", "HEAD"))

	ctx := context.Background()

	merged, err := IsAncestor(ctx, dir, baseCommit, featureCommit)
	if err != nil {
		t.Fatalf("IsAncestor() error = %v", err)
	}
	if !merged {
		t.Fatal("expected base commit to be reachable from feature commit")
	}

	merged, err = IsAncestor(ctx, dir, featureCommit, "main")
	if err != nil {
		t.Fatalf("IsAncestor() error = %v", err)
	}
	if merged {
		t.Fatal("expected feature commit to not be reachable from main")
	}
}

func TestIsAncestor_RequiresRefs(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	ctx := context.Background()

	_, err := IsAncestor(ctx, dir, "", "main")
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Fatalf("expected invalid input for empty ancestor, got %v", err)
	}

	_, err = IsAncestor(ctx, dir, "HEAD", " ")
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Fatalf("expected invalid input for empty descendant, got %v", err)
	}
}

func runOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
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

func TestMergedBranchesFromRef_ExcludesOnlyMatchingRemoteBaseBranch(t *testing.T) {
	dir := initBranchTestRepo(t, "feature/main")
	run := gitRunner(t, dir)

	run("branch", "main")

	run("checkout", "-b", "feature-done")
	if err := os.WriteFile(filepath.Join(dir, "done.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature done")
	run("checkout", "feature/main")
	run("merge", "feature-done")

	run("update-ref", "refs/remotes/origin/feature/main", "refs/heads/feature/main")
	run("checkout", "-b", "scratch")

	ctx := context.Background()
	branches, err := MergedBranchesFromRef(ctx, dir, "origin/feature/main")
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(branches)
	want := []string{"feature-done", "main"}
	if len(branches) != len(want) {
		t.Fatalf("expected %d merged branches, got %d: %v", len(want), len(branches), branches)
	}
	for i := range want {
		if branches[i] != want[i] {
			t.Fatalf("MergedBranchesFromRef() = %v, want %v", branches, want)
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

// initBranchTestRepoWithRemote sets up a working repo tracking a bare remote,
// so we can simulate the gone-upstream scenario that occurs when a PR is
// squash-merged and the remote source branch is deleted.
func initBranchTestRepoWithRemote(t *testing.T, defaultBranch string) (workDir, remoteDir string) {
	t.Helper()

	remoteDir = t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	mustRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
	mustRun(remoteDir, "init", "--bare", "-b", defaultBranch)

	workDir = initBranchTestRepo(t, defaultBranch)
	mustRun(workDir, "remote", "add", "origin", remoteDir)
	mustRun(workDir, "push", "-u", "origin", defaultBranch)
	return workDir, remoteDir
}

func TestGoneBranches_DetectsDeletedUpstream(t *testing.T) {
	workDir, remoteDir := initBranchTestRepoWithRemote(t, "main")
	run := gitRunner(t, workDir)

	// Branch with an upstream that will go away.
	run("checkout", "-b", "squash-merged-feature")
	run("commit", "--allow-empty", "-m", "feature work")
	run("push", "-u", "origin", "squash-merged-feature")

	// Another branch that keeps its upstream — must NOT be reported as gone.
	run("checkout", "-b", "still-open-feature")
	run("commit", "--allow-empty", "-m", "open work")
	run("push", "-u", "origin", "still-open-feature")

	// Simulate a squash-merge remote cleanup: delete the first branch
	// directly on the bare remote, then prune-fetch to refresh tracking.
	remoteCmd := exec.Command("git", "-C", remoteDir, "branch", "-D", "squash-merged-feature")
	if out, err := remoteCmd.CombinedOutput(); err != nil {
		t.Fatalf("delete remote branch: %v\n%s", err, out)
	}
	run("checkout", "main")
	run("fetch", "--prune", "origin")

	ctx := context.Background()
	branches, err := GoneBranches(ctx, workDir)
	if err != nil {
		t.Fatalf("GoneBranches: %v", err)
	}

	sort.Strings(branches)
	want := []string{"squash-merged-feature"}
	if len(branches) != len(want) || branches[0] != want[0] {
		t.Fatalf("GoneBranches() = %v, want %v", branches, want)
	}
}

func TestGoneBranches_NoGoneUpstreams(t *testing.T) {
	workDir, _ := initBranchTestRepoWithRemote(t, "main")
	run := gitRunner(t, workDir)

	run("checkout", "-b", "feature")
	run("commit", "--allow-empty", "-m", "feature")
	run("push", "-u", "origin", "feature")
	run("checkout", "main")

	ctx := context.Background()
	branches, err := GoneBranches(ctx, workDir)
	if err != nil {
		t.Fatalf("GoneBranches: %v", err)
	}
	if len(branches) != 0 {
		t.Fatalf("expected no gone branches, got %v", branches)
	}
}

func TestGoneBranches_ExcludesCurrentBranch(t *testing.T) {
	workDir, remoteDir := initBranchTestRepoWithRemote(t, "main")
	run := gitRunner(t, workDir)

	// Check out a tracking branch and make its upstream go away.
	run("checkout", "-b", "orphan")
	run("commit", "--allow-empty", "-m", "orphan")
	run("push", "-u", "origin", "orphan")

	remoteCmd := exec.Command("git", "-C", remoteDir, "branch", "-D", "orphan")
	if out, err := remoteCmd.CombinedOutput(); err != nil {
		t.Fatalf("delete remote branch: %v\n%s", err, out)
	}
	run("fetch", "--prune", "origin")
	// Stay on 'orphan' — it should be excluded from the result.

	ctx := context.Background()
	branches, err := GoneBranches(ctx, workDir)
	if err != nil {
		t.Fatalf("GoneBranches: %v", err)
	}
	for _, b := range branches {
		if b == "orphan" {
			t.Fatalf("GoneBranches returned the currently checked-out branch %q", b)
		}
	}
}

func TestGoneBranches_CancelledContext(t *testing.T) {
	dir := initBranchTestRepo(t, "main")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	branches, err := GoneBranches(ctx, dir)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if branches != nil {
		t.Fatalf("expected nil branches for cancelled context, got %v", branches)
	}
}

func TestDeleteBranchForce_RemovesSquashMergedBranch(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	run := gitRunner(t, dir)

	// Create a branch with a unique commit then squash-merge it —
	// i.e. apply its diff via a fresh commit on main without a merge
	// commit. The branch commit is NOT an ancestor of main, so -d would refuse.
	run("checkout", "-b", "feat-squash")
	if err := os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feat work")
	run("checkout", "main")
	// Apply the same content as a brand-new commit — simulates squash.
	if err := os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "squash of feat work")

	ctx := context.Background()
	// -d would refuse here; -D must succeed.
	if err := DeleteBranchForce(ctx, dir, "feat-squash"); err != nil {
		t.Fatalf("DeleteBranchForce: %v", err)
	}

	remaining, err := exec.Command("git", "-C", dir, "branch", "--list", "feat-squash").Output()
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	if strings.TrimSpace(string(remaining)) != "" {
		t.Fatalf("feat-squash branch still present: %q", string(remaining))
	}
}
