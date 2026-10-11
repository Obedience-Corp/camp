package dungeon

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

const goNeedsShell = "go needs shell integration"

// A status that answers one key press goes with the next key that changes
// what is on screen. It must not sit under a different row, lens, or page.
func TestExploreActionStatusDoesNotOutliveItsRow(t *testing.T) {
	feed := func() exploreModel {
		m := screenFeed(t, 100, 30, explore.ProtocolOff, 12)
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
	one := screenFeed(t, 100, 30, explore.ProtocolOff, 1)
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
		m := screenFeed(t, 80, 24, explore.ProtocolOff, 6)
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
	m := screenFeed(t, 100, 30, explore.ProtocolKitty, 6)
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
	late := screenFeed(t, 100, 30, explore.ProtocolKitty, 6)
	late.posterFor, late.posterReplay = item.Path, item.Replay
	late, _ = press(t, late, tea.KeyMsg{Type: tea.KeyEnter})
	late, _ = press(t, late, explorePoster{gen: late.loadGen, item: item.Path, err: context.DeadlineExceeded})
	if strings.Contains(late.View(), "could not be read") {
		t.Fatal("a replay failure surfaced inside the reader")
	}
	for name, reading := range map[string]exploreModel{"drawn before": m, "failed during": late} {
		closed, _ := press(t, reading, tea.KeyMsg{Type: tea.KeyEsc})
		if closed.reading || closed.status == "" || !strings.Contains(closed.View(), closed.status) {
			t.Errorf("%s: closing the reader lost the replay status (%q)", name, closed.status)
		}
		// A movement key that cannot move focus is not a reason to drop it.
		still, _ := press(t, closed, runes("k"))
		if still.status != closed.status {
			t.Errorf("%s: k on the first row cleared the replay status", name)
		}
		moved, _ := press(t, closed, runes("j"))
		if moved.status != "" {
			t.Errorf("%s: the replay status followed focus to another row: %q", name, moved.status)
		}
	}
}

func TestExploreReplayIsAskedForOnceAndKept(t *testing.T) {
	ready := func(w, h int) exploreModel {
		m := screenFeed(t, w, h, explore.ProtocolKitty, 6)
		m.reduced = false
		m.decode = func(context.Context, string, int, int) (explore.Frames, error) {
			return explore.Frames{}, context.Canceled
		}
		return m
	}

	// The model starts at 80x24. A terminal of that size reports no change in
	// geometry, and the focused replay still has to be requested.
	m := ready(80, 24)
	m, cmd := press(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	item, _ := m.focused()
	if cmd == nil || m.posterFor != item.Path {
		t.Fatal("the first size message at 80x24 did not ask for the focused replay")
	}
	gen := m.loadGen
	if m, cmd = press(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}); cmd != nil || m.loadGen != gen {
		t.Fatal("a repeated size message asked for the replay again")
	}

	// A failed refresh keeps the cached feed; its focused replay still draws.
	failed := ready(80, 24)
	failed, _ = press(t, failed, exploreLoaded{gen: failed.indexGen, err: context.DeadlineExceeded})
	if failed.loadGen == 0 || failed.posterFor != item.Path || !failed.statusErr {
		t.Fatalf("a failed refresh did not ask for the focused replay (status %q)", failed.status)
	}

	// Drawn and playing: a refresh that leaves the row alone must not blank
	// the replay and start it over.
	playing := ready(120, 40)
	playing, _ = press(t, playing, tea.WindowSizeMsg{Width: 120, Height: 40})
	poster := posterFor(item.Path, 64, 64)
	playing, _ = press(t, playing, explorePoster{
		gen: playing.loadGen, item: item.Path, poster: poster,
		frames: [][]byte{poster, poster}, delays: []time.Duration{time.Second, time.Second},
	})
	if !playing.playing || playing.poster == nil {
		t.Fatal("fixture: the replay is not playing")
	}
	for name, changed := range map[string]bool{"unchanged refresh": false, "refresh with other rows changed": true} {
		idx := playing.index
		if changed {
			idx.Items = append([]explore.Item(nil), idx.Items...)
			idx.Items[len(idx.Items)-1].Title = "renamed elsewhere"
		}
		after, _ := press(t, playing, exploreLoaded{gen: playing.indexGen, index: idx, changed: changed})
		if after.poster == nil || !after.playing || after.loadGen != playing.loadGen || after.tickGen != playing.tickGen {
			t.Errorf("%s restarted a replay whose row did not change", name)
		}
	}
	// Still decoding: the refresh must not cancel and restart it either.
	pending := ready(120, 40)
	pending, _ = press(t, pending, tea.WindowSizeMsg{Width: 120, Height: 40})
	if after, _ := press(t, pending, exploreLoaded{gen: pending.indexGen, index: pending.index}); after.loadGen != pending.loadGen || after.posterFor != item.Path {
		t.Error("an unchanged refresh restarted a decode that was still running")
	}

	// A different replay file for the focused row is a different replay.
	idx := playing.index
	idx.Items = append([]explore.Item(nil), idx.Items...)
	idx.Items[0].Replay = idx.Items[0].Path + "/second-replay.gif"
	swapped, _ := press(t, playing, exploreLoaded{gen: playing.indexGen, index: idx, changed: true})
	if swapped.loadGen == playing.loadGen || swapped.poster != nil || swapped.posterReplay != idx.Items[0].Replay {
		t.Error("a new replay file for the focused row was not decoded")
	}

	// r asks for the camp to be read again, the focused replay included.
	rescan, _ := press(t, playing, runes("r"))
	rescan, _ = press(t, rescan, exploreLoaded{gen: rescan.indexGen, index: playing.index, changed: true})
	if rescan.loadGen == playing.loadGen || rescan.posterFor != item.Path {
		t.Error("an explicit rescan did not decode the focused replay again")
	}
}

// In the narrow layout the replay sits between the focused row and its card.
// The card holds the only copy of the status, the date, and the path.
func TestExploreNarrowCardStaysVisibleUnderTheReplay(t *testing.T) {
	for _, size := range [][2]int{{80, 22}, {80, 24}, {80, 28}, {60, 24}, {87, 30}} {
		for _, status := range []string{"", "copied"} {
			m := screenFeed(t, size[0], size[1], explore.ProtocolKitty, 60)
			m.index.Warnings = []string{"festivals/.dungeon/archived: permission denied"}
			m.status = status
			m.cursor = 22
			for m.visible.Items[m.cursor].Replay == "" {
				m.cursor++
			}
			item, _ := m.focused()
			label := fmt.Sprintf("%dx%d status=%q", size[0], size[1], status)
			if m.wide() || m.stageRows() == 0 {
				t.Fatalf("%s: fixture is not the narrow layout with a replay", label)
			}
			rowOf := func(view string) int {
				for i, line := range strings.Split(view, "\n") {
					if strings.HasPrefix(viewText(line), "> "+item.Title) {
						return i
					}
				}
				return -1
			}
			before := rowOf(m.View())

			w, h := m.stagePixels()
			m.poster, m.mediaFor = posterFor(item.Path, w, h), item.Path
			view := m.View()
			text := viewText(view)
			for _, want := range []string{"moved ", item.Path, item.ID + "  Festivals  completed"} {
				if !strings.Contains(text, fit(want, m.width)) {
					t.Errorf("%s: the card lost %q once the replay drew:\n%s", label, want, text)
				}
			}
			if _, _, _, rows, ok := imageBox(view); !ok || rows != m.stageRows() {
				t.Errorf("%s: replay drawn on %d rows, want all %d", label, rows, m.stageRows())
			}
			if problems := checkLayout(m); len(problems) > 0 {
				t.Errorf("%s: %v", label, problems)
			}
			if after := rowOf(view); before < 0 || after != before {
				t.Errorf("%s: the focused row moved from line %d to %d when the replay arrived", label, before, after)
			}
		}
	}
}

func TestExploreListingSelectionSurvivesTheStatusRow(t *testing.T) {
	m := screenFeed(t, 80, 12, explore.ProtocolOff, 3)
	m.reading = true
	m.reader = exploreReader{title: "docs", listing: true, item: m.visible.Items[0]}
	for i := range 40 {
		m.reader.entries = append(m.reader.entries, readerEntry{name: fmt.Sprintf("doc-%02d.md", i)})
	}
	selected := func(m exploreModel) string {
		for _, line := range strings.Split(m.View(), "\n") {
			if text := viewText(line); strings.HasPrefix(text, "> ") {
				return strings.TrimSpace(strings.TrimPrefix(text, "> "))
			}
		}
		return ""
	}
	for range m.bodyHeight() - 1 {
		m, _ = press(t, m, runes("j"))
	}
	want := m.reader.entries[m.reader.entry].name
	if m.reader.entry != m.reader.offset+m.bodyHeight()-1 || selected(m) != want {
		t.Fatalf("fixture: selection %q is not on the last body row", want)
	}
	// Any source can put the status row there between two key presses.
	m.setStatus(exploreActionStatus, "copied", false)
	if got := selected(m); got != want {
		t.Fatalf("status row pushed the selection out of view: showing %q, want %q\n%s", got, want, m.View())
	}
	if problems := checkLayout(m); len(problems) > 0 {
		t.Fatal(problems)
	}
	m.setStatus(exploreActionStatus, "", false)
	if got := selected(m); got != want {
		t.Fatalf("selection lost when the status row left: %q", got)
	}
}

func hintKey(hint helpHint) string {
	return hint.text[:strings.LastIndex(hint.text, " ")]
}

// Every key is reachable from a help row at 80 columns and wider, the feed
// row points at the full list, and the command help names the same keys.
func TestExploreEveryKeyIsDiscoverable(t *testing.T) {
	feedKeys := []string{"j/k", "ctrl+u/d", "enter", "g", "[ ]", "s", "space", "/", "y", "r", "?", "q"}
	readerKeys := []string{"j/k", "ctrl+u/d", "enter", "g", "y", "esc"}
	has := func(rows []string, key string) bool {
		for _, row := range rows {
			for _, hint := range strings.Split(viewText(row), exploreHelpSep) {
				if strings.HasPrefix(hint, key+" ") {
					return true
				}
			}
		}
		return false
	}
	for _, width := range []int{80, 88, 100, 120, 200} {
		m := screenFeed(t, width, 24, explore.ProtocolOff, 6)
		short := m.helpLines()
		if len(short) != 1 || !has(short, "?") || !has(short, "q") {
			t.Errorf("%d columns: the feed row %q does not point at the full help", width, short)
		}
		m.help = true
		full := m.helpLines()
		if len(full) > 2 {
			t.Errorf("%d columns: full help takes %d rows", width, len(full))
		}
		for _, key := range feedKeys {
			if !has(full, key) {
				t.Errorf("%d columns: full help does not name %q: %q", width, key, full)
			}
		}
		for _, row := range full {
			if w := lipgloss.Width(row); w > width {
				t.Errorf("%d columns: help row is %d wide: %q", width, w, row)
			}
		}
		if problems := checkLayout(m); len(problems) > 0 {
			t.Errorf("%d columns with full help: %v", width, problems)
		}
		m.help = false
		m.reading = true
		reader := m.helpLines()
		for _, key := range readerKeys {
			if !has(reader, key) {
				t.Errorf("%d columns: reader help does not name %q: %q", width, key, reader)
			}
		}
	}

	// The command help lists the same keys, feed and reader each under its
	// own heading, spelled as the help rows spell them.
	sections := longKeySections(dungeonExploreCmd.Long)
	for heading, table := range map[string][]helpHint{"Keys in the feed:": exploreFullHelp, "Keys in the reader:": exploreReaderHelp} {
		for _, hint := range table {
			if key := hintKey(hint); !sections[heading][key] {
				t.Errorf("camp dungeon explore --help: %q does not list the key %q", heading, key)
			}
		}
	}

	// ? opens the list and ? or esc closes it; a short terminal keeps a body row.
	m := screenFeed(t, 80, 4, explore.ProtocolOff, 6)
	m, _ = press(t, m, runes("?"))
	if !m.help || len(m.helpLines()) != 2 {
		t.Fatalf("? on a four-row terminal: help=%v rows=%d", m.help, len(m.helpLines()))
	}
	m.setStatus(exploreActionStatus, "copied", false)
	if len(m.helpLines()) != 1 || len(checkLayout(m)) > 0 {
		t.Fatalf("four rows with a status: help rows=%d problems=%v", len(m.helpLines()), checkLayout(m))
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.help || m.quitting {
		t.Fatal("esc did not close the full help")
	}
}

// longKeySections reads the key tables out of the command's long help: for
// each "Keys in ..." heading, the keys and aliases in the first column.
func longKeySections(long string) map[string]map[string]bool {
	sections := map[string]map[string]bool{}
	heading := ""
	for _, line := range strings.Split(long, "\n") {
		switch {
		case strings.HasPrefix(line, "Keys in "):
			heading = line
			sections[heading] = map[string]bool{}
		case heading != "" && strings.HasPrefix(line, "  "):
			keys, _, _ := strings.Cut(strings.TrimSpace(line), "   ")
			for _, key := range strings.Split(keys, ", ") {
				sections[heading][key] = true
			}
		case line != "":
			heading = ""
		}
	}
	return sections
}

// Space toggles a replay the reader does not draw. It is not a reader key.
func TestExploreReaderIgnoresSpace(t *testing.T) {
	m := screenFeed(t, 100, 30, explore.ProtocolKitty, 6)
	m.reduced = false
	item, _ := m.focused()
	poster := posterFor(item.Path, 64, 64)
	m.posterFor, m.posterReplay = item.Path, item.Replay
	m, _ = press(t, m, explorePoster{gen: m.loadGen, item: item.Path, poster: poster, frames: [][]byte{poster, poster}, delays: []time.Duration{time.Second, time.Second}})
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.reading || !m.playing {
		t.Fatalf("fixture: reading=%v playing=%v", m.reading, m.playing)
	}
	after, cmd := press(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if !after.playing || cmd != nil || after.tickGen != m.tickGen {
		t.Fatal("space in the reader paused a replay that is not on screen")
	}
}

func TestExploreEmptyStatesSayWhatHappened(t *testing.T) {
	query := explore.Query{Status: "finished", Dungeon: explore.LensAll}
	// A first load that fails with no cache knows nothing about the camp.
	m := exploreModel{width: 80, height: 24, loading: true, protocol: explore.ProtocolOff, query: query}
	m, _ = press(t, m, exploreLoaded{gen: m.indexGen, err: context.DeadlineExceeded})
	view := viewText(m.View())
	if strings.Contains(view, "No dungeons in this camp.") {
		t.Fatalf("a failed first load claims the camp has no dungeons:\n%s", view)
	}
	if !strings.Contains(view, "could not be read") || !strings.Contains(view, context.DeadlineExceeded.Error()) {
		t.Fatalf("a failed first load does not say so:\n%s", view)
	}
	// r retries, and a camp that really has no dungeons says that.
	m, cmd := press(t, m, runes("r"))
	if cmd == nil || !m.loading {
		t.Fatal("r did not retry the failed load")
	}
	m, _ = press(t, m, exploreLoaded{gen: m.indexGen, index: explore.Index{BuiltAt: time.Now()}, changed: true})
	if view := viewText(m.View()); !strings.Contains(view, "No dungeons in this camp.") || strings.Contains(view, "could not be read") {
		t.Fatalf("an empty camp after a successful load:\n%s", view)
	}

	// No empty-state row is cut mid-word by the renderer.
	states := map[string]exploreModel{
		"no dungeons": {query: query},
		"load failed": {query: query, loadFailed: true},
		"first load":  {query: query, loading: true},
		"no finished work": {query: query, index: explore.Index{
			Dungeons: []explore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
		}},
		"no match": {query: explore.Query{Status: "archived", Dungeon: explore.LensAll}, index: explore.Index{
			Dungeons: []explore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
		}},
	}
	for name, state := range states {
		for _, width := range []int{20, 24} {
			state.width, state.height, state.protocol = width, 6, explore.ProtocolOff
			if err := state.applyQuery(); err != nil {
				t.Fatal(err)
			}
			body := strings.Split(state.View(), "\n")[1]
			if w := lipgloss.Width(body); w > width || !strings.HasSuffix(body, "…") {
				t.Errorf("%s at %d columns: body row %q (%d wide) is not fitted", name, width, body, w)
			}
		}
	}
}

func TestExploreFilterPromptKeepsTheTypedEnd(t *testing.T) {
	m := screenFeed(t, 40, 12, explore.ProtocolOff, 6)
	m, _ = press(t, m, runes("/"))
	typed := ""
	for i := range 30 {
		word := fmt.Sprintf("w%02d界 ", i)
		typed += word
		m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(word), Paste: true})
		header := strings.Split(m.View(), "\n")[0]
		if w := lipgloss.Width(header); w > m.width {
			t.Fatalf("filter prompt is %d columns wide after %q", w, typed)
		}
		if tail := fmt.Sprintf("w%02d界", i); !strings.HasPrefix(header, "/ ") || !strings.HasSuffix(strings.TrimRight(header, " "), tail) {
			t.Fatalf("filter prompt %q does not end with the text just typed (%q)", header, tail)
		}
	}
	if !strings.Contains(strings.Split(m.View(), "\n")[0], "…") {
		t.Fatal("a cut filter does not mark the cut")
	}
	short := screenFeed(t, 40, 12, explore.ProtocolOff, 6)
	short, _ = press(t, short, runes("/"))
	short, _ = press(t, short, runes("FA00"))
	if header := strings.Split(short.View(), "\n")[0]; header != "/ FA00" {
		t.Fatalf("short filter prompt = %q", header)
	}
}
