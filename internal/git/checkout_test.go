package git

import (
	"context"
	"testing"
)

func TestBranchExists_LocalBranchOnly(t *testing.T) {
	dir := initBranchTestRepo(t, "main")
	run := gitRunner(t, dir)

	run("tag", "develop")

	if BranchExists(context.Background(), dir, "develop") {
		t.Fatal("BranchExists() = true for tag-only ref, want false")
	}

	run("checkout", "-b", "develop")
	run("checkout", "main")

	if !BranchExists(context.Background(), dir, "develop") {
		t.Fatal("BranchExists() = false for local branch, want true")
	}
}
