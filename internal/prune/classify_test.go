//go:build !integration

package prune

import "testing"

func TestParseStatusV2(t *testing.T) {
	out := "1 .M N... 100644 100644 100644 aaa bbb keep me.txt\x00" +
		"1 .M S.M. 160000 160000 160000 aaa aaa vendor/lib\x00" +
		"1 M. SC.. 160000 160000 160000 aaa bbb vendor/pinned\x00" +
		"2 R. N... 100644 100644 100644 aaa bbb R100 new name.txt\x00old name.txt\x00" +
		"? untracked dir/file.txt\x00"
	got := parseStatusV2(out)
	want := []statusChange{
		{path: "keep me.txt"},
		{path: "vendor/lib", submoduleDirty: true},
		{path: "vendor/pinned"},
		{path: "new name.txt"},
		{path: "untracked dir/file.txt"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d changes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(parseStatusV2("")) != 0 {
		t.Error("a clean worktree should report no changes")
	}
}

func TestSavedRefNameKeepsNestedBranchesApart(t *testing.T) {
	a, ab, pct := savedRefName("a"), savedRefName("a/b"), savedRefName("a%2Fb")
	if a != SavedRefPrefix+"a" || ab != SavedRefPrefix+"a%2Fb" {
		t.Fatalf("unexpected names %q, %q", a, ab)
	}
	if ab == pct {
		t.Fatalf("%q and %q must not share a ref", "a/b", "a%2Fb")
	}
}
