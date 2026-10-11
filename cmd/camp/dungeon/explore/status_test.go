package explore

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

const goNeedsShell = "go needs shell integration"

// A status that answers one key press goes with the next key that changes
// what is on screen. It must not sit under a different row, lens, or page.
func TestExploreActionStatusDoesNotOutliveItsRow(t *testing.T) {
	feed := func() exploreModel {
		m := screenFeed(t, 100, 30, intexplore.ProtocolOff, 12)
		m, _ = press(t, m, runes("g"))
		if !m.statusErr || !strings.Contains(m.status, goNeedsShell) {
			t.Fatalf("g without shell integration set %q", m.status)
		}
		return m
	}
	steps := map[string]func(exploreModel) exploreModel{
		"status lens":  func(m exploreModel) exploreModel { m, _ = press(t, m, runes("s")); return m },
		"next dungeon": func(m exploreModel) exploreModel { m, _ = press(t, m, runes("]")); return m },
		"previous dungeon": func(m exploreModel) exploreModel {
			m, _ = press(t, m, runes("["))
			return m
		},
		"filter applied": func(m exploreModel) exploreModel {
			m, _ = press(t, m, runes("/"))
			m, _ = press(t, m, runes("Item 03"))
			m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			return m
		},
		"focus moved": func(m exploreModel) exploreModel { m, _ = press(t, m, runes("j")); return m },
		"reader opened": func(m exploreModel) exploreModel {
			m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if !m.reading {
				t.Fatal("enter did not open the reader")
			}
			return m
		},
		"refresh moved focus": func(m exploreModel) exploreModel {
			idx := m.index
			idx.Items = idx.Items[1:]
			m, _ = press(t, m, exploreLoaded{gen: m.indexGen, index: idx, changed: true})
			return m
		},
	}
	for name, step := range steps {
		if got := step(feed()); got.status != "" || got.statusErr {
			t.Errorf("%s: status is still %q", name, got.status)
		}
	}

	// The cursor has nowhere to go on a feed of one row; the key still clears.
	one := screenFeed(t, 100, 30, intexplore.ProtocolOff, 1)
	one, _ = press(t, one, runes("g"))
	for _, key := range []string{"j", "k"} {
		moved, _ := press(t, one, runes(key))
		if moved.status != "" {
			t.Errorf("one-row feed: %s left %q", key, moved.status)
		}
	}

	// A refresh that leaves the focused row alone is not a key press.
	kept := feed()
	kept, _ = press(t, kept, exploreLoaded{gen: kept.indexGen, index: kept.index, changed: true})
	if !strings.Contains(kept.status, goNeedsShell) {
		t.Errorf("a background refresh on the same row cleared %q", kept.status)
	}
}

func TestExploreReaderStatusClearsOnTheNextKey(t *testing.T) {
	open := func(listing bool) exploreModel {
		m := screenFeed(t, 80, 24, intexplore.ProtocolOff, 6)
		m.reading = true
		m.reader = exploreReader{title: "docs", item: m.visible.Items[0], listing: listing}
		for i := range 30 {
			m.reader.entries = append(m.reader.entries, readerEntry{name: fmt.Sprintf("doc-%02d.md", i)})
			m.reader.lines = append(m.reader.lines, fmt.Sprintf("line %02d", i))
		}
		return m
	}
	keys := map[string]tea.Msg{
		"j": runes("j"), "k": runes("k"), "ctrl+d": tea.KeyMsg{Type: tea.KeyCtrlD}, "ctrl+u": tea.KeyMsg{Type: tea.KeyCtrlU},
		"esc": tea.KeyMsg{Type: tea.KeyEsc},
	}
	for _, listing := range []bool{true, false} {
		for _, source := range []exploreStatusSource{exploreActionStatus, exploreReadStatus} {
			for name, key := range keys {
				m := open(listing)
				m.setStatus(source, "The file could not be parsed.", true)
				m, _ = press(t, m, key)
				if m.status != "" || m.statusErr {
					t.Errorf("listing=%v source=%d: %s left %q", listing, source, name, m.status)
				}
			}
		}
	}
	// g in the reader still reports, and the report stays until the next key.
	m := open(false)
	m, _ = press(t, m, runes("g"))
	if !strings.Contains(m.status, goNeedsShell) || !strings.Contains(m.View(), goNeedsShell) {
		t.Fatalf("g in the reader reported %q", m.status)
	}
}

// A replay status describes the focused feed row. The reader does not draw
// the replay, so the status waits for the reader to close instead of being
// shown over a document or lost.
func TestExploreReplayStatusWaitsOutTheReader(t *testing.T) {
	const tooLong = "Replay is too long to play here."
	m := screenFeed(t, 100, 30, intexplore.ProtocolKitty, 6)
	item, _ := m.focused()
	m.posterFor, m.posterReplay = item.Path, item.Replay
	m, _ = press(t, m, explorePoster{gen: m.loadGen, item: item.Path, poster: posterFor(item.Path, 64, 64), tooLong: true})
	if !strings.Contains(m.View(), tooLong) {
		t.Fatalf("feed does not report the replay:\n%s", m.View())
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.reading {
		t.Fatal("enter did not open the reader")
	}
	if strings.Contains(m.View(), tooLong) {
		t.Fatal("the reader shows a status about a replay it does not draw")
	}
	if problems := checkLayout(m); len(problems) > 0 {
		t.Fatalf("reader with a hidden status: %v", problems)
	}
	// It also lands hidden when the decode finishes after the reader opened.
	late := screenFeed(t, 100, 30, intexplore.ProtocolKitty, 6)
	late.posterFor, late.posterReplay = item.Path, item.Replay
	late, _ = press(t, late, tea.KeyMsg{Type: tea.KeyEnter})
	late, _ = press(t, late, explorePoster{gen: late.loadGen, item: item.Path, err: context.DeadlineExceeded})
	if strings.Contains(late.View(), "could not be read") {
		t.Fatal("a replay failure surfaced inside the reader")
	}
	for name, reading := range map[string]exploreModel{"drawn before": m, "failed during": late} {
		closed, _ := press(t, reading, tea.KeyMsg{Type: tea.KeyEsc})
		note := closed.shownStatus()
		if closed.reading || note == "" || !strings.Contains(closed.View(), note) {
			t.Errorf("%s: closing the reader lost the replay status (%q)", name, note)
		}
		// A movement key that cannot move focus is not a reason to drop it.
		still, _ := press(t, closed, runes("k"))
		if still.shownStatus() != note {
			t.Errorf("%s: k on the first row cleared the replay status", name)
		}
		moved, _ := press(t, closed, runes("j"))
		if moved.shownStatus() != "" {
			t.Errorf("%s: the replay status followed focus to another row: %q", name, moved.shownStatus())
		}
	}
}

// A rescan that is running or failed, a query that does not resolve, and a
// note about the focused replay are state. A one-shot status covers them for
// one key press; it must not erase them.
func TestExploreStateNotesOutliveOneShotStatuses(t *testing.T) {
	copyKey := func(m exploreModel) exploreModel {
		m.setStatus(exploreActionStatus, "copied", false)
		return m
	}

	// A failed refresh over a cached feed: the feed on screen is stale.
	m := screenFeed(t, 100, 30, intexplore.ProtocolOff, 12)
	m, _ = press(t, m, exploreLoaded{gen: m.indexGen, err: context.DeadlineExceeded})
	failure := context.DeadlineExceeded.Error()
	if row := m.statusRow(); row.text != failure || !row.failed {
		t.Fatalf("failed refresh shows %+v", row)
	}
	m = copyKey(m)
	if m.shownStatus() != "copied" {
		t.Fatalf("the answer to a key press is not shown first: %q", m.shownStatus())
	}
	for _, key := range []tea.Msg{runes("j"), runes("k"), runes("s"), tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyEsc}} {
		m, _ = press(t, copyKey(m), key)
		if row := m.statusRow(); row.text != failure || !row.failed || !strings.Contains(viewText(m.View()), failure) {
			t.Fatalf("after %v the stale feed no longer says the refresh failed: %+v", key, row)
		}
	}
	m, _ = press(t, m, runes("r"))
	m, _ = press(t, m, exploreLoaded{gen: m.indexGen, index: m.index, changed: true})
	if m.shownStatus() != "" {
		t.Fatalf("a successful rescan left %q", m.shownStatus())
	}

	// A rescan in progress.
	m = screenFeed(t, 100, 30, intexplore.ProtocolOff, 12)
	m, _ = press(t, m, runes("r"))
	m, _ = press(t, copyKey(m), runes("j"))
	if m.shownStatus() != exploreReadingStatus || !m.loading {
		t.Fatalf("a running rescan shows %q after a copy and a move", m.shownStatus())
	}

	// A note about the focused replay, covered by g without shell integration.
	m = screenFeed(t, 100, 30, intexplore.ProtocolKitty, 6)
	item, _ := m.focused()
	m.posterFor, m.posterReplay = item.Path, item.Replay
	m, _ = press(t, m, explorePoster{gen: m.loadGen, item: item.Path, poster: posterFor(item.Path, 64, 64), tooLong: true})
	note := m.shownStatus()
	m, _ = press(t, m, runes("g"))
	if !strings.Contains(m.shownStatus(), goNeedsShell) {
		t.Fatalf("g shows %q", m.shownStatus())
	}
	m, _ = press(t, m, runes("k"))
	if m.shownStatus() != note || note == "" {
		t.Fatalf("the replay note for the row still focused is %q, want %q", m.shownStatus(), note)
	}
	// A lens change that keeps the row keeps its replay and its note.
	gen := m.loadGen
	m, _ = press(t, m, runes("/"))
	m, _ = press(t, m, runes(item.Title))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if now, _ := m.focused(); now.Path != item.Path || m.loadGen != gen || m.poster == nil || m.shownStatus() != note {
		t.Fatalf("filtering to the focused row restarted its replay or lost its note: gen %d to %d, status %q", gen, m.loadGen, m.shownStatus())
	}
	// A filter that drops the row moves focus, and the note goes with the row.
	m, _ = press(t, m, runes("/"))
	for range item.Title {
		m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m, _ = press(t, m, runes("Item 03"))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if now, ok := m.focused(); !ok || now.Path == item.Path || m.shownStatus() != "" || m.poster != nil {
		t.Fatalf("the replay and its note followed focus to another row: %q", m.shownStatus())
	}

	// A query that does not resolve outranks a load note under it.
	m = screenFeed(t, 100, 30, intexplore.ProtocolOff, 12)
	m.query.Dungeon = "missing"
	_ = m.applyQuery()
	m, _ = press(t, m, runes("r"))
	if row := m.statusRow(); !row.failed || !strings.Contains(row.text, "missing") {
		t.Fatalf("an unresolved query is hidden by the rescan: %+v", row)
	}
	if problems := checkLayout(m); len(problems) > 0 {
		t.Fatal(problems)
	}
}

func TestExploreRescanClearsReadingStatus(t *testing.T) {
	m := replayModel(intexplore.ProtocolOff)
	idx := m.index
	m, _ = press(t, m, runes("r"))
	if m.shownStatus() == "" {
		t.Fatal("rescan did not report that it is reading")
	}
	m, _ = press(t, m, exploreLoaded{gen: m.indexGen, index: idx, changed: true})
	if m.shownStatus() != "" {
		t.Fatalf("status after a successful rescan = %q", m.shownStatus())
	}
	if strings.Contains(m.View(), exploreReadingStatus) {
		t.Fatal("the rescan message is still on screen")
	}
}
