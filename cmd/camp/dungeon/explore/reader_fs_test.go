//go:build container_fs

package explore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/unix"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

func TestExploreReadFailures(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing")} {
		if _, _, err := readExploreText(context.Background(), path); err == nil {
			t.Fatalf("reading %s silently succeeded", path)
		}
	}
	path := filepath.Join(root, "goal.md")
	want := strings.Repeat("a", 256<<10) + "beyond limit"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	if got, cut, err := readExploreText(context.Background(), path); err != nil || !cut || got != want[:256<<10] {
		t.Fatalf("bounded read: %d bytes, cut %v, %v", len(got), cut, err)
	}
	m := replayModel(intexplore.ProtocolOff)
	m.root = root
	m.visible.Items[0].Path = "."
	m.visible.Items[0].IsDir = true
	if err := os.WriteFile(filepath.Join(root, "FESTIVAL_GOAL.md"), []byte("# Goal\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A goal file that is there and cannot be read: the read is cancelled.
	stopped, stop := context.WithCancel(context.Background())
	stop()
	m.ctx = stopped
	next, _ := m.openReader()
	m = next.(exploreModel)
	if m.reading || !m.statusErr {
		t.Fatal("unreadable goal opened a blank reader")
	}
}

func TestExploreReaderRejectsSpecialFiles(t *testing.T) {
	root := t.TempDir()
	normal := filepath.Join(root, "README.md")
	if err := os.WriteFile(normal, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "pending.md")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(normal, filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	entries, err := markdownEntries(root)
	if err != nil || len(entries) != 1 || entries[0].name != "README.md" {
		t.Fatalf("entries: %+v, %v", entries, err)
	}
	// A file selected in a previous directory listing may have been replaced.
	m := replayModel(intexplore.ProtocolOff)
	m.reading = true
	m.reader = exploreReader{listing: true, entries: []readerEntry{{name: "pending.md", path: fifo}}}
	done := make(chan exploreModel, 1)
	go func() { next, _ := m.onReaderKey(tea.KeyMsg{Type: tea.KeyEnter}); done <- next.(exploreModel) }()
	select {
	case next := <-done:
		if !next.statusErr || !next.reader.listing {
			t.Fatal("special file opened in reader")
		}
	case <-time.After(time.Second):
		t.Fatal("reader blocked on FIFO")
	}
}

func TestExploreReaderClearsParseError(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "readme.md")
	if err := os.WriteFile(good, []byte("# Hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := replayModel(intexplore.ProtocolOff)
	m.reading = true
	m.reader = exploreReader{
		listing: true,
		entries: []readerEntry{
			{name: "missing.md", path: filepath.Join(root, "missing.md")},
			{name: "readme.md", path: good},
		},
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.statusErr || m.status != "The file could not be parsed." || !m.reader.listing {
		t.Fatalf("unreadable entry status=%q err=%v listing=%v", m.status, m.statusErr, m.reader.listing)
	}
	stuck := m
	m, _ = press(t, m, runes("j"))
	if m.statusErr || m.status != "" || m.reader.entry != 1 {
		t.Fatalf("the failure followed the cursor: status=%q entry=%d", m.status, m.reader.entry)
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.statusErr || m.status != "" || m.reader.listing {
		t.Fatalf("readable document still shows %q err=%v listing=%v", m.status, m.statusErr, m.reader.listing)
	}
	// Opening a readable entry clears the failure even when the cursor did
	// not move, as when the file is fixed and opened again.
	stuck.reader.entries[0].path = good
	stuck, _ = press(t, stuck, tea.KeyMsg{Type: tea.KeyEnter})
	if stuck.statusErr || stuck.status != "" || stuck.reader.listing {
		t.Fatalf("reopened document still shows %q err=%v listing=%v", stuck.status, stuck.statusErr, stuck.reader.listing)
	}
	if strings.Contains(m.View(), "could not be parsed") || !strings.Contains(m.View(), "Hello") {
		t.Fatal("the reader kept the parse error or dropped the document")
	}

	// A status from an earlier key press, such as a copy, describes the
	// listing. It does not stay over the document that opens next.
	for _, earlier := range []struct {
		text   string
		failed bool
	}{{"copied", false}, {"copy failed: denied", true}} {
		opened := replayModel(intexplore.ProtocolOff)
		opened.reading = true
		opened.reader = exploreReader{listing: true, entries: []readerEntry{{name: "readme.md", path: good}}}
		opened.setStatus(exploreActionStatus, earlier.text, earlier.failed)
		opened, _ = press(t, opened, tea.KeyMsg{Type: tea.KeyEnter})
		if opened.reader.listing || opened.status != "" || opened.statusErr {
			t.Fatalf("opened document still shows %q err=%v listing=%v", opened.status, opened.statusErr, opened.reader.listing)
		}
	}
}

// A document longer than the reader loads must say that the rest is missing.
func TestExploreReaderSaysWhenADocumentIsCut(t *testing.T) {
	root := t.TempDir()
	// The limit falls one byte into a three-byte character.
	long := "# Long\n\n"
	long += strings.Repeat("word ", readerLimit/5)[:readerLimit-len(long)-1] + "界界界 the end\n"
	small := "# Small\n\nAll of it.\n"
	for name, text := range map[string]string{"long.md": long, "small.md": small, "FESTIVAL_GOAL.md": long} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const notice = "Showing the first 256 KiB of this file."
	text, cut, err := readExploreText(context.Background(), filepath.Join(root, "long.md"))
	if err != nil || !cut || len(text) != readerLimit-1 || !utf8.ValidString(text) || strings.Contains(text, "界") {
		t.Fatalf("long read: %d bytes, cut %v, valid %v, %v", len(text), cut, utf8.ValidString(text), err)
	}
	if text, cut, err := readExploreText(context.Background(), filepath.Join(root, "small.md")); err != nil || cut || text != small {
		t.Fatalf("small read: %q, cut %v, %v", text, cut, err)
	}

	open := func(item intexplore.Item) exploreModel {
		m := replayModel(intexplore.ProtocolOff)
		m.root = root
		m.visible.Items[0] = item
		m.cursor = 0
		m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if !m.reading {
			t.Fatalf("enter did not open %s: %q", item.Path, m.status)
		}
		return m
	}
	// The top of the screen, as text.
	top := func(m exploreModel) string {
		lines := strings.Split(m.View(), "\n")
		for i, line := range lines {
			lines[i] = strings.TrimRight(viewText(line), " ")
		}
		return strings.Join(lines[:8], "\n")
	}
	file := open(intexplore.Item{Kind: intexplore.KindMarkdown, Title: "long", Path: "long.md"})
	if got := top(file); !strings.Contains(got, notice) || !strings.Contains(got, "Long") {
		t.Fatalf("a cut document does not say so: %q", got)
	}
	whole := open(intexplore.Item{Kind: intexplore.KindMarkdown, Title: "small", Path: "small.md"})
	if got := top(whole); strings.Contains(got, notice) || !strings.Contains(got, "All of it.") {
		t.Fatalf("a whole document claims to be cut: %q", got)
	}
	goal := open(intexplore.Item{Kind: intexplore.KindFestival, Title: "festival", Path: ".", IsDir: true})
	if got := top(goal); !strings.Contains(got, notice) || !strings.Contains(got, "No replay yet.") {
		t.Fatalf("a cut festival goal lost a notice: %q", got)
	}
	listing := open(intexplore.Item{Kind: intexplore.KindWorkitem, Title: "workitem", Path: ".", IsDir: true})
	for i, entry := range listing.reader.entries {
		if entry.name == "long.md" {
			listing.reader.entry = i
		}
	}
	listing, _ = press(t, listing, tea.KeyMsg{Type: tea.KeyEnter})
	if got := top(listing); listing.reader.listing || !strings.Contains(got, notice) {
		t.Fatalf("a cut listing entry does not say so: %q", got)
	}
}

// The index reads FESTIVAL_GOAL.md only when it is a regular file. The reader
// must agree: a link, a directory, or a pipe of that name is a festival with
// no goal file, and Enter still opens it on the summary.
func TestExploreReaderOpensFestivalWithoutARegularGoalFile(t *testing.T) {
	stage := map[string]func(t *testing.T, goal string){
		"symlink": func(t *testing.T, goal string) {
			target := filepath.Join(filepath.Dir(filepath.Dir(goal)), "shared-goal.md")
			if err := os.WriteFile(target, []byte("# Linked\n\nText from outside the festival.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, goal); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T, goal string) {
			if err := os.Mkdir(goal, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"pipe": func(t *testing.T, goal string) {
			if err := unix.Mkfifo(goal, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, put := range stage {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			festival := filepath.Join(root, "festival")
			if err := os.Mkdir(festival, 0o700); err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(festival, "FESTIVAL_GOAL.md"))

			m := replayModel(intexplore.ProtocolOff)
			m.root = root
			m.visible.Items[0].Path = "festival"
			m.visible.Items[0].IsDir = true
			summary := m.visible.Items[0].Summary
			next, _ := m.openReader()
			m = next.(exploreModel)
			if !m.reading || m.statusErr {
				t.Fatalf("Enter did not open the festival: reading %v, status %q", m.reading, m.status)
			}
			text := strings.Join(m.reader.lines, "\n")
			if !strings.Contains(strings.Join(strings.Fields(text), " "), summary) {
				t.Fatalf("reader does not show the summary: %q", text)
			}
			if strings.Contains(text, "outside the festival") {
				t.Fatalf("reader followed the link: %q", text)
			}

			m.visible.Items[0].Summary = ""
			next, _ = m.openReader()
			if got := strings.Join(next.(exploreModel).reader.lines, "\n"); !strings.Contains(got, "No festival goal file.") {
				t.Fatalf("reader with no summary = %q", got)
			}
		})
	}
}
