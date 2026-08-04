package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTreeSHAMatchesWriteTreeForHEAD(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
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
	headTree, err := TreeSHA(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("TreeSHA(HEAD): %v", err)
	}
	indexTree, err := WriteTree(ctx, dir)
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}
	if headTree != indexTree {
		t.Fatalf("clean index tree %s != HEAD tree %s", indexTree, headTree)
	}

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "b.txt")
	stagedTree, err := WriteTree(ctx, dir)
	if err != nil {
		t.Fatalf("WriteTree after stage: %v", err)
	}
	if stagedTree == headTree {
		t.Fatal("staged change must produce a different tree than HEAD")
	}
}
