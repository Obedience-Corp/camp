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

// A name can hold nothing a terminal shows. The row still needs something to
// read and to select, so the name is quoted with its characters escaped.
func TestTidyQuotesANameWithNothingToShow(t *testing.T) {
	for name, want := range map[string]string{
		"\x01\x02":           `"\x01\x02"`,
		"\u200b\u202e":       `"\u200b\u202e"`,
		"\x1b[31m":           `"\x1b[31m"`,
		" ":                  `" "`,
		"\u200b\x07.md":      ".md",
		"shipped-\x1b[1mone": "shipped-one",
	} {
		item := Item{}
		tidy(&item, name)
		if item.Title != want {
			t.Errorf("name %q: title = %q, want %q", name, item.Title, want)
		}
		if strings.TrimSpace(item.Title) == "" || item.Title != CleanText(item.Title) {
			t.Errorf("name %q: title %q is blank or carries what CleanText removes", name, item.Title)
		}
	}
	long := Item{}
	tidy(&long, strings.Repeat("\x01", 10*titleLimit))
	if n := utf8.RuneCountInString(long.Title); n != titleLimit {
		t.Errorf("a quoted name holds %d runes, want it cut to %d", n, titleLimit)
	}
}

type fakeEntry struct {
	name string
	err  error
}

func (e fakeEntry) Name() string      { return e.name }
func (e fakeEntry) IsDir() bool       { return false }
func (e fakeEntry) Type() fs.FileMode { return 0 }
func (e fakeEntry) Info() (fs.FileInfo, error) {
	if e.err != nil {
		return nil, e.err
	}
	return fakeInfo{mode: 0o644, mod: time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local), size: 3}, nil
}

// A directory is listed, then each entry is looked at. One removed in between
// is not a row; one that is there and cannot be looked at still is.
func TestReadItemSkipsAnEntryRemovedAfterItWasListed(t *testing.T) {
	const root = "/camp"
	read := func(entry fakeEntry) (Item, string, bool) {
		path := root + "/festivals/.dungeon/completed/" + entry.name
		return readItem(context.Background(), root, "festivals/.dungeon", "Festivals", "completed", "", path, entry)
	}

	gone := &fs.PathError{Op: "lstat", Path: root + "/festivals/.dungeon/completed/moved.txt", Err: syscall.ENOENT}
	if item, warning, ok := read(fakeEntry{name: "moved.txt", err: gone}); ok || warning != "" {
		t.Fatalf("a removed entry = %+v, warning %q, row %v; want no row and no warning", item, warning, ok)
	}

	denied := &fs.PathError{Op: "lstat", Path: root + "/festivals/.dungeon/completed/locked.txt", Err: syscall.EACCES}
	item, warning, ok := read(fakeEntry{name: "locked.txt", err: denied})
	if !ok || item.Path != "festivals/.dungeon/completed/locked.txt" || item.Warning != "permission denied" {
		t.Fatalf("an entry that cannot be read = %+v, row %v; want its row with the reason", item, ok)
	}
	if warning != "festivals/.dungeon/completed/locked.txt: permission denied" {
		t.Fatalf("warning = %q", warning)
	}

	item, warning, ok = read(fakeEntry{name: "kept.txt"})
	if !ok || warning != "" || item.Title != "kept.txt" || item.DoneDate != "2026-10-05" {
		t.Fatalf("an ordinary entry = %+v, warning %q, row %v", item, warning, ok)
	}
}

// An entry that cannot be examined has no file time to date it by. It keeps
// its bucket's day when it sits in one, and otherwise has no done date; it is
// never dated today, which would put it first in the feed.
func TestReadItemLeavesAnEntryItCannotExamineUndated(t *testing.T) {
	const root = "/camp"
	denied := func(path string) error {
		return &fs.PathError{Op: "lstat", Path: root + "/" + path, Err: syscall.EACCES}
	}

	loose := "festivals/.dungeon/completed/locked.txt"
	item, _, ok := readItem(context.Background(), root, "festivals/.dungeon", "Festivals", "completed", "", root+"/"+loose, fakeEntry{name: "locked.txt", err: denied(loose)})
	if !ok || item.DoneDate != "" || item.DateSource != DateUnknown || item.Warning != "permission denied" {
		t.Fatalf("row = %+v, row %v; want no done date, date source %q, and the reason", item, ok, DateUnknown)
	}

	bucketed := "festivals/.dungeon/completed/2026-09-01/locked.txt"
	item, _, ok = readItem(context.Background(), root, "festivals/.dungeon", "Festivals", "completed", "2026-09-01", root+"/"+bucketed, fakeEntry{name: "locked.txt", err: denied(bucketed)})
	if !ok || item.DoneDate != "2026-09-01" || item.DateSource != DateBucket {
		t.Fatalf("bucketed row = %+v, row %v; want the bucket's day", item, ok)
	}
}
