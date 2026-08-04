package cmdutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNothingStagedLine(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "init")

	ctx := context.Background()

	got := NothingStagedLine(ctx, dir, "")
	if got != "Nothing to commit, working tree clean" {
		t.Fatalf("clean tree: got %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = NothingStagedLine(ctx, dir, "")
	if got != "Nothing staged to commit" {
		t.Fatalf("untracked dirt: got %q", got)
	}

	got = NothingStagedLine(ctx, dir, " in project")
	if got != "Nothing staged to commit in project" {
		t.Fatalf("scoped dirty: got %q", got)
	}

	// Stage then unstage via reset so we have unstaged tracked changes.
	run("add", "b.txt")
	run("reset", "HEAD", "--", "b.txt")
	// b.txt is now untracked again after reset of a never-committed add...
	// Write over a tracked file instead for a true unstaged modification.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = NothingStagedLine(ctx, dir, "")
	if !strings.Contains(got, "Nothing staged to commit") {
		t.Fatalf("unstaged tracked change: got %q", got)
	}

	// Clean project-scoped message when nothing is dirty.
	run("checkout", "--", "a.txt")
	_ = os.Remove(filepath.Join(dir, "b.txt"))
	got = NothingStagedLine(ctx, dir, " in worktree")
	if got != "Nothing to commit in worktree" {
		t.Fatalf("clean scoped: got %q", got)
	}
}
