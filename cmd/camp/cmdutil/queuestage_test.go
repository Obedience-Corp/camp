package cmdutil

import (
	"context"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/shellcmd"
	"github.com/Obedience-Corp/camp/internal/stageguard"
)

func sizes(m map[string]int64) func(string) (int64, bool) {
	return func(rel string) (int64, bool) {
		size, ok := m[rel]
		return size, ok
	}
}

func noLFS(_ context.Context, _ string, c []stageguard.Candidate) ([]stageguard.Candidate, error) {
	return c, nil
}

var autoLimits = stageguard.GuardLimits{
	MaxFileSize:   10 << 20,
	LargeFiles:    stageguard.ModeAuto,
	Bulk:          stageguard.ModeBlock,
	MaxAddedFiles: 1000,
}

func TestPlanAutoWriteTakesRootContentFromThePlan(t *testing.T) {
	plan := git.RootPlan{
		Commit: []stageguard.Candidate{{Path: "videos/my-video/todo.md", Size: 20, Untracked: true}},
		Kept:   []stageguard.Candidate{{Path: "videos/my-video/broll.mp4", Size: 3 << 20, Untracked: true}},
	}
	paths, newFiles, foreground := planAutoWrite(
		[]string{"notes.md"},
		[]string{"videos/my-video/broll.mp4", "videos/my-video/takes/", "videos/my-video/todo.md"},
		[]string{"videos/my-video"},
		[]string{"projects/camp"},
		plan,
		sizes(map[string]int64{"notes.md": 20}),
	)
	if foreground {
		t.Fatal("entries inside a declared root are the plan's to decide, not the foreground's")
	}
	want := []string{"notes.md", "videos/my-video/todo.md"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if len(newFiles) != 1 || newFiles[0] != "videos/my-video/todo.md" {
		t.Fatalf("newFiles = %v, want the root's new note only", newFiles)
	}
	tally := plan.Tally([]string{"videos/my-video"})
	if len(tally) != 1 || tally[0].Files != 1 || tally[0].Bytes != 3<<20 || len(tally[0].NestedRepos) != 0 {
		t.Fatalf("tally = %+v, want only the kept-out b-roll", tally)
	}
	plan.Moved = []git.RootMove{{From: "notes/broll.mp4", To: "videos/my-video/broll.mp4", Size: 3 << 20}}
	if tally := plan.Tally([]string{"videos/my-video"}); len(tally) != 0 {
		t.Fatalf("tally = %+v, a tracked file moved into the root is reported on its own, not as media", tally)
	}
}

func TestPlanAutoWriteCommitsAMoveIntoANewDirectoryInARoot(t *testing.T) {
	plan := git.RootPlan{
		Commit: []stageguard.Candidate{{Path: "videos/my-video/drafts/readme.md", Size: 20, Untracked: true}},
	}
	paths, _, foreground := planAutoWrite(
		[]string{"notes/readme.md"},
		[]string{"videos/my-video/drafts/"},
		[]string{"videos/my-video"},
		nil,
		plan,
		sizes(nil),
	)
	if foreground {
		t.Fatal("a new directory inside a declared root must not force the full guard")
	}
	if len(paths) != 2 || paths[1] != "videos/my-video/drafts/readme.md" {
		t.Fatalf("paths = %v", paths)
	}
	if tally := plan.Tally([]string{"videos/my-video"}); len(tally) != 0 {
		t.Fatalf("tally = %+v, a directory whose only file is committed must not say kept out", tally)
	}
}

func TestPlanAutoWriteStopsForAnUntrackedDirectory(t *testing.T) {
	for _, dir := range []string{"vendor/", ".campaign/skills/demo/"} {
		_, _, foreground := planAutoWrite(nil, []string{dir}, nil, nil, git.RootPlan{}, sizes(nil))
		if !foreground {
			t.Fatalf("%s outside a declared root must reach the guard", dir)
		}
	}
}

func TestPlanAutoWriteSkipsSubmodules(t *testing.T) {
	paths, _, foreground := planAutoWrite(
		[]string{"projects/camp", "readme.md"},
		nil,
		nil,
		[]string{"projects/camp"},
		git.RootPlan{},
		sizes(map[string]int64{"readme.md": 10}),
	)
	if foreground {
		t.Fatal("a submodule pointer must be skipped, not treated as new content")
	}
	if len(paths) != 1 || paths[0] != "readme.md" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestDecideFastPathSendsANewLargeFileToTheForeground(t *testing.T) {
	in := fastPathInputs{
		untracked: []string{"releases/big.bin"},
		limits:    autoLimits,
		sizeOf:    sizes(map[string]int64{"releases/big.bin": 30 << 20}),
	}
	if _, _, ok := decideFastPath(context.Background(), "", in, noLFS); ok {
		t.Fatal("a new over-threshold file must stay in the foreground so the report prints")
	}
	in.limits.Allow = []string{"releases/**"}
	if paths, _, ok := decideFastPath(context.Background(), "", in, noLFS); !ok || len(paths) != 1 {
		t.Fatalf("an allowed large file has nothing to report; paths = %v, ok = %v", paths, ok)
	}
}

func TestDecideFastPathFallsBackWhenTheLFSCheckFails(t *testing.T) {
	failing := func(context.Context, string, []stageguard.Candidate) ([]stageguard.Candidate, error) {
		return nil, camperrors.New("check-attr failed")
	}
	in := fastPathInputs{
		tracked: []string{"videos/my-video/script.md"},
		limits:  autoLimits,
		sizeOf:  sizes(map[string]int64{"videos/my-video/script.md": 30 << 20}),
	}
	if _, _, ok := decideFastPath(context.Background(), "", in, failing); ok {
		t.Fatal("a failed LFS check must send the commit to the foreground, which reports it")
	}
	if _, _, ok := decideFastPath(context.Background(), "", in, func(context.Context, string, []stageguard.Candidate) ([]stageguard.Candidate, error) {
		return nil, nil
	}); !ok {
		t.Fatal("a path the LFS check removes has nothing to report")
	}
}

func TestDecideFastPathRespectsTheBulkThreshold(t *testing.T) {
	limits := autoLimits
	limits.MaxAddedFiles = 3
	files := map[string]int64{}
	var untracked []string
	for _, name := range []string{"notes/a.md", "notes/b.md", "notes/c.md"} {
		files[name] = 10
		untracked = append(untracked, name)
	}
	below := fastPathInputs{untracked: untracked[:2], limits: limits, sizeOf: sizes(files)}
	if _, _, ok := decideFastPath(context.Background(), "", below, noLFS); !ok {
		t.Fatal("two new files under a three-file bulk threshold stay on the fast path")
	}
	at := fastPathInputs{untracked: untracked, limits: limits, sizeOf: sizes(files)}
	if _, _, ok := decideFastPath(context.Background(), "", at, noLFS); ok {
		t.Fatal("new files reaching the bulk threshold must go to the foreground guard")
	}
	inRoot := fastPathInputs{
		untracked: untracked[:1],
		roots:     []string{"videos/my-video"},
		plan: git.RootPlan{Commit: []stageguard.Candidate{
			{Path: "videos/my-video/a.md", Size: 10, Untracked: true},
			{Path: "videos/my-video/b.md", Size: 10, Untracked: true},
		}},
		limits: limits,
		sizeOf: sizes(files),
	}
	if _, _, ok := decideFastPath(context.Background(), "", inRoot, noLFS); ok {
		t.Fatal("new small files inside a root count toward the bulk threshold")
	}
	off := at
	off.limits.Bulk = stageguard.ModeOff
	if _, _, ok := decideFastPath(context.Background(), "", off, noLFS); !ok {
		t.Fatal("bulk: off disables the threshold")
	}
}

func TestOverThresholdKeepsQuietPathsQuiet(t *testing.T) {
	sizeOf := func(string) (int64, bool) { return 30 << 20, true }
	paths := []string{"videos/my-video/script.md", "releases/big.bin"}
	allowed := autoLimits
	allowed.Allow = []string{"videos/my-video/**", "*.bin"}
	if over := overThreshold(paths, sizeOf, allowed); len(over) != 0 {
		t.Fatalf("over = %+v, commit.guards.allow matches must not be reported", over)
	}
	off := autoLimits
	off.LargeFiles = stageguard.ModeOff
	if over := overThreshold(paths, sizeOf, off); len(over) != 0 {
		t.Fatalf("over = %+v, large_files: off reports nothing", over)
	}
	partial := autoLimits
	partial.Allow = []string{"*.bin"}
	over := overThreshold(paths, sizeOf, partial)
	if len(over) != 1 || over[0].Path != "videos/my-video/script.md" || over[0].Size != 30<<20 {
		t.Fatalf("over = %+v, want only the path the allowlist does not cover", over)
	}
}

func TestQuotePathOnlyQuotesWhatAShellWouldRead(t *testing.T) {
	cases := map[string]string{
		"videos/my-video/model.bin": "videos/my-video/model.bin",
		"videos/my video/take.mov":  shellcmd.QuoteLiteral("videos/my video/take.mov"),
		"it's.mp4":                  shellcmd.QuoteLiteral("it's.mp4"),
		"*.mp4":                     shellcmd.QuoteLiteral("*.mp4"),
	}
	for in, want := range cases {
		if got := quotePath(in); got != want {
			t.Errorf("quotePath(%q) = %q, want %q", in, got, want)
		}
	}
}
