//go:build !integration

package prune

import (
	"testing"
	"time"
)

func TestParseStatusV2(t *testing.T) {
	out := "1 .M N... 100644 100644 100644 aaa bbb keep me.txt\x00" +
		"1 .M S.M. 160000 160000 160000 aaa aaa vendor/lib\x00" +
		"1 M. SC.. 160000 160000 160000 aaa bbb vendor/pinned\x00" +
		"2 R. N... 100644 100644 100644 aaa bbb R100 new name.txt\x00old name.txt\x00" +
		"? untracked dir/file.txt\x00" +
		"! .env.local\x00" +
		"! node_modules/\x00"
	got := parseStatusV2(out)
	want := []statusChange{
		{path: "keep me.txt"},
		{path: "vendor/lib", submoduleDirty: true},
		{path: "vendor/pinned"},
		{path: "new name.txt"},
		{path: "untracked dir/file.txt"},
		{path: ".env.local", ignored: true},
		{path: "node_modules/", ignored: true},
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

func TestSplitIgnoredKeepsIgnoredPathsOutOfTheChanges(t *testing.T) {
	changes, ignored := splitIgnored(parseStatusV2("! node_modules/\x00! .env.local\x00"))
	if len(changes) != 0 {
		t.Fatalf("ignored paths alone must not make a worktree dirty: %+v", changes)
	}
	if len(ignored) != 2 {
		t.Fatalf("got %d ignored paths, want 2", len(ignored))
	}
}

func TestIgnoredDirsNote(t *testing.T) {
	if got := ignoredDirsNote(nil); got != "" {
		t.Errorf("no folders should give no note, got %q", got)
	}
	if got, want := ignoredDirsNote([]string{"bin/", "dist/"}), "ignored folders not saved: bin/, dist/"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got := ignoredDirsNote([]string{"a/", "b/", "c/", "d/", "e/", "f/"})
	if want := "ignored folders not saved: a/, b/, c/, d/ and 2 more"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseSavedRefs(t *testing.T) {
	out := "refs/camp/pruned/new\tbbb\t200\ncamp prune: uncommitted edits from new\n\n" +
		"Camp-Pruned-Branch: new\nCamp-Pruned-Worktree: /wt/new\n\x00\n" +
		"refs/camp/pruned/a%2Fb\taaa\t100\nsubject\n\nCamp-Pruned-Branch: a/b\nCamp-Pruned-Worktree: /wt/a b\n\x00\n" +
		"refs/camp/pruned/stray\tccc\t300\nnot written by camp\n\x00"
	got := parseSavedRefs(out)
	if len(got) != 2 {
		t.Fatalf("got %d saved entries, want 2: %+v", len(got), got)
	}
	if got[0].Branch != "a/b" || got[0].Worktree != "/wt/a b" || got[0].Commit != "aaa" || !got[0].SavedAt.Equal(time.Unix(100, 0)) {
		t.Errorf("oldest entry = %+v", got[0])
	}
	if got[1].Branch != "new" || got[1].Ref != "refs/camp/pruned/new" || !got[1].Expires.Equal(time.Unix(200, 0).Add(SavedRetention)) {
		t.Errorf("newest entry = %+v", got[1])
	}
}
