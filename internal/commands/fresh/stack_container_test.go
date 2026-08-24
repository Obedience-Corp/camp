//go:build container_fs

// Filesystem-mutating tests for stack cleanup.
//
// These build only under the container_fs tag and are executed inside the
// integration harness's pooled container (see tests/integration/containerfs_test.go),
// never on the host. The tag is the enforcement seam: `just test` on a
// developer machine does not compile this file, so nothing here can create a
// repo in someone's home directory.

package fresh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
)

const stackTarget = "feat/aggregate"

func TestPlanStackCleanup_ClassifiesWorktrees(t *testing.T) {
	fx := setupStackFixture(t)
	ctx := context.Background()

	plan, err := planStackCleanup(ctx, fx.repo, stackTarget, false)
	if err != nil {
		t.Fatalf("planStackCleanup: %v", err)
	}

	remove := planBranchSet(plan.remove)
	dirty := planBranchSet(plan.skipDirty)
	unmerged := planBranchSet(plan.skipUnmerged)
	offStack := planBranchSet(plan.skipOffStack)
	keep := planBranchSet(plan.keep)

	mustHave(t, "remove", remove, "child-clean", "child-squash")
	mustHave(t, "skipDirty", dirty, "child-dirty")
	mustHave(t, "skipUnmerged", unmerged, "child-unmerged")
	mustHave(t, "skipOffStack", offStack, "from-main")
	mustNotHave(t, "remove", remove, "child-dirty", "child-unmerged", "from-main", stackTarget)
	if _, ok := keep[stackTarget]; !ok && !keepHasPath(plan.keep, fx.repo) {
		t.Errorf("target/primary should be kept; keep branches=%v", keysOfSet(keep))
	}
}

func TestPlanStackCleanup_RefusesDefaultBranch(t *testing.T) {
	fx := setupStackFixture(t)
	_, err := planStackCleanup(context.Background(), fx.repo, "main", false)
	if err == nil {
		t.Fatal("expected refusal when targeting the default branch")
	}
	if !strings.Contains(err.Error(), "refuses the default branch") {
		t.Errorf("error = %q", err)
	}
}

func TestPlanStackCleanup_AllowDefaultTarget(t *testing.T) {
	fx := setupStackFixture(t)
	plan, err := planStackCleanup(context.Background(), fx.repo, "main", true)
	if err != nil {
		t.Fatalf("planStackCleanup with --allow-default-target: %v", err)
	}
	remove := planBranchSet(plan.remove)
	mustHave(t, "remove", remove, "from-main")
	mustNotHave(t, "remove", remove, "child-clean", "child-squash", "child-unmerged", "child-dirty")
}

func TestExecuteStackCleanup_RemovesMergedChildren(t *testing.T) {
	fx := setupStackFixture(t)
	ctx := context.Background()

	plan, err := planStackCleanup(ctx, fx.repo, stackTarget, false)
	if err != nil {
		t.Fatalf("planStackCleanup: %v", err)
	}
	if len(plan.remove) < 2 {
		t.Fatalf("expected ancestry + squash removes, got %d: %v", len(plan.remove), keysOfSet(planBranchSet(plan.remove)))
	}

	removed, errs := executeStackCleanup(ctx, plan)
	if len(errs) > 0 {
		t.Fatalf("executeStackCleanup errors: %v", errs)
	}
	if removed != len(plan.remove) {
		t.Fatalf("removed %d, want %d", removed, len(plan.remove))
	}

	assertMissing(t, fx, "child-clean")
	assertMissing(t, fx, "child-squash")
	assertPresent(t, fx, "child-dirty")
	assertPresent(t, fx, "child-unmerged")
	assertPresent(t, fx, "from-main")
	if !branchExists(t, fx.repo, stackTarget) {
		t.Error("target branch was deleted")
	}
	if _, err := os.Stat(fx.repo); err != nil {
		t.Errorf("primary path missing: %v", err)
	}
}

func TestBranchesEquivalentToRef_SquashAndAncestry(t *testing.T) {
	fx := setupStackFixture(t)
	eq, err := git.BranchesEquivalentToRef(context.Background(), fx.repo, stackTarget,
		[]string{"child-squash", "child-clean", "child-unmerged"})
	if err != nil {
		t.Fatalf("BranchesEquivalentToRef: %v", err)
	}
	if _, ok := eq["child-squash"]; !ok {
		t.Error("squash-merged child should be equivalent via cumulative patch-id")
	}
	if _, ok := eq["child-clean"]; !ok {
		t.Error("ancestry-merged child should be equivalent")
	}
	if _, ok := eq["child-unmerged"]; ok {
		t.Error("unmerged child should not be equivalent")
	}
}

func TestRunStackCleanup_DryRunMakesNoChanges(t *testing.T) {
	fx := setupStackFixture(t)
	ctx := context.Background()

	plan, err := planStackCleanup(ctx, fx.repo, stackTarget, false)
	if err != nil {
		t.Fatalf("planStackCleanup: %v", err)
	}
	if len(plan.remove) == 0 {
		t.Fatal("fixture produced an empty remove set; dry-run would be a no-op")
	}

	if err := runStackCleanup(ctx, "proj", fx.repo, stackTarget, true, false, "  "); err != nil {
		t.Fatalf("runStackCleanup dry-run: %v", err)
	}

	for _, branch := range []string{"child-clean", "child-squash", "child-dirty", "child-unmerged", "from-main", stackTarget} {
		assertPresent(t, fx, branch)
	}
}

type stackFixture struct {
	repo  string
	paths map[string]string
}

func setupStackFixture(t *testing.T) *stackFixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wts := filepath.Join(root, "wts")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wts, 0o755); err != nil {
		t.Fatal(err)
	}

	run := gitRunner(t, repo)
	run("init", "-b", "main")
	run("config", "user.email", "t@t.co")
	run("config", "user.name", "T")
	run("config", "commit.gpgsign", "false")
	writeFile(t, repo, "README.md", "# test\n")
	run("add", ".")
	run("commit", "-m", "initial")

	// Feature that landed on main (not a stack child of the later aggregate).
	run("checkout", "-b", "from-main")
	writeFile(t, repo, "from-main.txt", "main feature\n")
	run("add", ".")
	run("commit", "-m", "from-main work")
	run("checkout", "main")
	run("merge", "--no-ff", "from-main", "-m", "merge from-main")

	run("checkout", "-b", stackTarget)
	writeFile(t, repo, "aggregate.txt", "aggregate\n")
	run("add", ".")
	run("commit", "-m", "start aggregate")

	paths := map[string]string{}
	addChild := func(name string) string {
		p := filepath.Join(wts, name)
		run("worktree", "add", "-b", name, p, stackTarget)
		paths[name] = p
		return p
	}

	clean := addChild("child-clean")
	writeFile(t, clean, "child-clean.txt", "clean\n")
	gitRunner(t, clean)("add", ".")
	gitRunner(t, clean)("commit", "-m", "child-clean work")
	run("merge", "--no-ff", "child-clean", "-m", "merge child-clean")

	squash := addChild("child-squash")
	writeFile(t, squash, "squash.txt", "one\n")
	gitRunner(t, squash)("add", ".")
	gitRunner(t, squash)("commit", "-m", "squash part one")
	writeFile(t, squash, "squash.txt", "one\ntwo\n")
	gitRunner(t, squash)("add", ".")
	gitRunner(t, squash)("commit", "-m", "squash part two")
	run("merge", "--squash", "child-squash")
	run("commit", "-m", "squash child-squash")

	dirty := addChild("child-dirty")
	writeFile(t, dirty, "child-dirty.txt", "dirty-base\n")
	gitRunner(t, dirty)("add", ".")
	gitRunner(t, dirty)("commit", "-m", "child-dirty work")
	run("merge", "--no-ff", "child-dirty", "-m", "merge child-dirty")
	writeFile(t, dirty, "uncommitted.txt", "leave me\n")

	unmerged := addChild("child-unmerged")
	writeFile(t, unmerged, "unmerged.txt", "still open\n")
	gitRunner(t, unmerged)("add", ".")
	gitRunner(t, unmerged)("commit", "-m", "child-unmerged work")

	fromMain := filepath.Join(wts, "from-main")
	run("worktree", "add", fromMain, "from-main")
	paths["from-main"] = fromMain

	return &stackFixture{repo: repo, paths: paths}
}

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

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func planBranchSet(plans []stackWorktreePlan) map[string]struct{} {
	out := make(map[string]struct{}, len(plans))
	for _, p := range plans {
		out[p.entry.Branch] = struct{}{}
	}
	return out
}

func keepHasPath(plans []stackWorktreePlan, path string) bool {
	want := filepath.Clean(path)
	for _, p := range plans {
		if filepath.Clean(p.entry.Path) == want {
			return true
		}
	}
	return false
}

func mustHave(t *testing.T, label string, set map[string]struct{}, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, ok := set[n]; !ok {
			t.Errorf("%s missing %q (have %v)", label, n, keysOfSet(set))
		}
	}
}

func mustNotHave(t *testing.T, label string, set map[string]struct{}, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, ok := set[n]; ok {
			t.Errorf("%s unexpectedly contains %q (have %v)", label, n, keysOfSet(set))
		}
	}
}

func keysOfSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

func branchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

func worktreePresent(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func assertPresent(t *testing.T, fx *stackFixture, branch string) {
	t.Helper()
	if !branchExists(t, fx.repo, branch) {
		t.Errorf("branch %q missing", branch)
	}
	if path, ok := fx.paths[branch]; ok && !worktreePresent(t, path) {
		t.Errorf("worktree for %q missing at %s", branch, path)
	}
}

func assertMissing(t *testing.T, fx *stackFixture, branch string) {
	t.Helper()
	if branchExists(t, fx.repo, branch) {
		t.Errorf("branch %q still present", branch)
	}
	if path, ok := fx.paths[branch]; ok && worktreePresent(t, path) {
		t.Errorf("worktree for %q still present at %s", branch, path)
	}
}
