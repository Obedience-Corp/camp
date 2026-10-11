package explore

import (
	"context"
	"io/fs"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

func TestApplySinceUntil(t *testing.T) {
	items := []Item{
		{DoneDate: "2026-10-01", Title: "a", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "completed"},
		{DoneDate: "2026-10-05", Title: "b", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "completed"},
	}
	got, err := Apply(Index{Items: items}, Query{Since: "2026-10-05", Until: "2026-10-05"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Title != "b" {
		t.Fatalf("items = %+v", got.Items)
	}
	if _, err := Apply(Index{Items: items}, Query{Since: "2026-10-06", Until: "2026-10-01"}); err == nil {
		t.Fatal("since after until error = nil")
	}
}

type fakeInfo struct {
	mode fs.FileMode
	mod  time.Time
	size int64
}

func (f fakeInfo) Name() string       { return "fest.yaml" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return f.mod }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

// A chmod changes neither size nor modification time, and it decides whether
// the file can be read at all.
func TestStampCarriesTheFileMode(t *testing.T) {
	mod := time.Date(2026, 10, 5, 16, 30, 18, 0, time.UTC)
	readable := stamp("completed/fa/fest.yaml", fakeInfo{mode: 0o644, mod: mod, size: 120})
	if again := stamp("completed/fa/fest.yaml", fakeInfo{mode: 0o644, mod: mod, size: 120}); again != readable {
		t.Fatalf("the same file stamped as %q and %q", readable, again)
	}
	for name, info := range map[string]fakeInfo{
		"permissions removed": {mode: 0, mod: mod, size: 120},
		"replaced by a link":  {mode: fs.ModeSymlink | 0o644, mod: mod, size: 120},
	} {
		if got := stamp("completed/fa/fest.yaml", info); got == readable {
			t.Errorf("%s: stamp is still %q", name, got)
		}
	}
}

func TestTidyBoundsWhatARowCarries(t *testing.T) {
	long := strings.Repeat("\u00e9", 300_000)
	item := Item{Title: long, ID: long, Summary: long, Warning: long}
	tidy(&item, "note.md")
	for field, got := range map[string]struct {
		text  string
		limit int
	}{
		"title":   {item.Title, titleLimit},
		"id":      {item.ID, idLimit},
		"summary": {item.Summary, summaryLimit},
		"warning": {item.Warning, warningLimit},
	} {
		if n := utf8.RuneCountInString(got.text); n != got.limit {
			t.Errorf("%s holds %d runes, want it cut to %d", field, n, got.limit)
		}
		if !utf8.ValidString(got.text) {
			t.Errorf("%s was cut inside a character", field)
		}
	}
	short := Item{Title: "Ship it", ID: "FA0024", Summary: "One  line.", Warning: "fest.yaml: permission denied"}
	tidy(&short, "ship-it")
	if short.Title != "Ship it" || short.ID != "FA0024" || short.Summary != "One line." || short.Warning != "fest.yaml: permission denied" {
		t.Errorf("ordinary text was changed: %+v", short)
	}
}

func TestTidyFallsBackToTheNameForABlankTitle(t *testing.T) {
	for name, title := range map[string]string{
		"empty":             "",
		"zero width":        "\u200b\u202e",
		"only an escape":    "\x1b[31m\x1b[0m",
		"only white space":  " \t ",
		"only a line break": "\n",
	} {
		item := Item{Title: title}
		tidy(&item, "shipped-\x1b[1mnote.md")
		if item.Title != "shipped-note.md" {
			t.Errorf("%s: title = %q, want the cleaned file name", name, item.Title)
		}
	}
	kept := Item{Title: "\u200bReal title"}
	tidy(&kept, "note.md")
	if kept.Title != "Real title" {
		t.Errorf("title = %q, want the frontmatter title kept", kept.Title)
	}
}

func TestAddWarningsBoundsTheList(t *testing.T) {
	var have []string
	for i := range 5 * maxIndexWarnings {
		have = addWarnings(have, "dir-"+strconv.Itoa(i)+": permission denied")
	}
	if len(have) != maxIndexWarnings {
		t.Fatalf("list holds %d warnings, want it to stop at %d", len(have), maxIndexWarnings)
	}
	if have[0] != "dir-0: permission denied" || have[maxIndexWarnings-1] != "dir-"+strconv.Itoa(maxIndexWarnings-1)+": permission denied" {
		t.Fatalf("the first warnings were not the ones kept: %q … %q", have[0], have[maxIndexWarnings-1])
	}
	one := addWarnings(nil, strings.Repeat("p/", 100_000)+": permission denied")
	if len(one) != 1 || utf8.RuneCountInString(one[0]) != warningLimit {
		t.Fatalf("one warning holds %d runes, want %d", utf8.RuneCountInString(one[0]), warningLimit)
	}
}

// A directory without search permission reports every file below it as
// unreachable, not missing.
func TestUnsearchableItemDirectoryWarnsOnItsRow(t *testing.T) {
	const locked = "/camp/festivals/.dungeon/completed/2026-10-01/locked-LK0001"
	orig := lstat
	lstat = func(path string) (fs.FileInfo, error) {
		if strings.HasPrefix(path, locked+"/") {
			return nil, &fs.PathError{Op: "lstat", Path: path, Err: syscall.EACCES}
		}
		return nil, &fs.PathError{Op: "lstat", Path: path, Err: syscall.ENOENT}
	}
	t.Cleanup(func() { lstat = orig })

	item := Item{Title: "locked-LK0001", Kind: KindOther}
	fillDirectory(context.Background(), &item, locked)
	if item.Warning != "locked-LK0001: permission denied" {
		t.Errorf("warning = %q, want the directory named without its absolute path", item.Warning)
	}
	if item.Kind != KindOther || item.Title != "locked-LK0001" {
		t.Errorf("row = %+v, want it left as the directory name", item)
	}

	plain := Item{Title: "plain", Kind: KindOther}
	fillDirectory(context.Background(), &plain, "/camp/festivals/.dungeon/completed/2026-10-01/plain")
	if plain.Warning != "" || plain.Title != "plain" {
		t.Errorf("a directory with no metadata files = %+v, want no warning", plain)
	}
}
