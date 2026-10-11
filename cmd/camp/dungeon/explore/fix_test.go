package explore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

const replayItemPath = "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024"

func replayModel(protocol string) exploreModel {
	m := exploreModel{
		width:    100,
		height:   30,
		protocol: protocol,
		query:    intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll},
		index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon"}},
			Items: []intexplore.Item{
				{
					DoneDate: "2026-10-05", DateSource: intexplore.DateBucket, Status: "completed",
					DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival,
					Title: "festival-activity-desktop", ID: "FA0024", Summary: "Ship Festival Activity as a signed desktop app for every platform we support today",
					Path: replayItemPath, Replay: replayItemPath + "/festival-replay.gif", IsDir: true,
				},
				{
					DoneDate: "2026-08-21", DateSource: intexplore.DateHistory, Status: "done",
					DungeonLabel: "Intents", DungeonPath: ".campaign/intents/.dungeon", Kind: intexplore.KindMarkdown,
					Title: "Add blog sections", Path: ".campaign/intents/.dungeon/done/add-blog.md",
				},
			},
		},
		mediaFor: replayItemPath,
		poster:   []byte{1, 2, 3, 4},
	}
	if err := m.applyQuery(); err != nil {
		panic(err)
	}
	return m
}

func press(t *testing.T, m exploreModel, msg tea.Msg) (exploreModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	out, ok := next.(exploreModel)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	return out, cmd
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestExploreViewStripsTerminalControlsFromRepoText(t *testing.T) {
	m := exploreModel{
		width:    100,
		height:   30,
		protocol: intexplore.ProtocolOff,
		query:    intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll},
		index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon"}},
			Items: []intexplore.Item{{
				DoneDate: "2026-10-05", DateSource: intexplore.DateBucket, Status: "completed",
				DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindOther,
				Title:   "evil\x1b]52;c;ZXZpbA==\x07title",
				ID:      "FA\x1b[31m01",
				Summary: "sum\x1b]0;pwned\x07mary\rline",
				Path:    "festivals/.dungeon/completed/evil\x1b[2Jpath",
			}},
		},
	}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	reading, _ := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	for name, view := range map[string]string{"feed": m.View(), "reader": reading.View()} {
		// The wide layout places its right pane with cursor control of its
		// own. Nothing else may reach the terminal as a control.
		view = viewPanePlacement.ReplaceAllString(view, "")
		if strings.ContainsAny(view, "\x1b\x07\r") {
			t.Fatalf("%s view carries terminal controls: %q", name, view)
		}
	}
	if view := m.View(); !strings.Contains(view, "eviltitle") || !strings.Contains(view, "summary") {
		t.Fatalf("sanitizing dropped printable text:\n%s", view)
	}
}

func TestExploreImageSequencePreservesCursor(t *testing.T) {
	kitty := replayModel(intexplore.ProtocolKitty).imageSequence(48)
	if !strings.HasPrefix(kitty, "\x1b7") || !strings.HasSuffix(kitty, "\x1b8") {
		t.Fatalf("kitty sequence does not save and restore the cursor: %q", kitty)
	}
	if !strings.Contains(kitty, "C=1") {
		t.Fatalf("kitty sequence lets the terminal move the cursor: %q", kitty)
	}
	iterm := replayModel(intexplore.ProtocolITerm).imageSequence(48)
	if !strings.HasPrefix(iterm, "\x1b7\x1b]1337;File=") || !strings.HasSuffix(iterm, "\a\x1b8") {
		t.Fatalf("iterm sequence does not save and restore the cursor: %q", iterm)
	}
}

func TestExploreDeletesKittyImageWhenHidden(t *testing.T) {
	del := intexplore.KittyDelete(exploreImageID)
	shown := replayModel(intexplore.ProtocolKitty)
	if !strings.Contains(shown.View(), "a=T,f=100") {
		t.Fatal("focused replay did not draw")
	}
	moved, _ := press(t, shown, runes("j"))
	reading, _ := press(t, shown, tea.KeyMsg{Type: tea.KeyEnter})
	small, _ := press(t, shown, tea.WindowSizeMsg{Width: 10, Height: 3})
	quit := shown
	quit.quitting = true
	for name, view := range map[string]string{
		"moved to an item without a replay": moved.View(),
		"opened the reader":                 reading.View(),
		"terminal too small":                small.View(),
		"quitting":                          quit.View(),
	} {
		if !strings.Contains(view, del) {
			t.Errorf("%s: view does not delete the Kitty placement", name)
		}
		if strings.Contains(view, "a=T,f=100") {
			t.Errorf("%s: view still draws the replay", name)
		}
	}
	if off := replayModel(intexplore.ProtocolOff); strings.Contains(off.View(), "\x1b_G") {
		t.Error("images off still emits Kitty graphics commands")
	}
}

func TestExploreFilterAcceptsRunesAndPaste(t *testing.T) {
	m := replayModel(intexplore.ProtocolOff)
	m, _ = press(t, m, runes("/"))
	m, _ = press(t, m, runes("é"))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("FA00\n24"), Paste: true})
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m, _ = press(t, m, runes("x"))
	if m.filter != "éFA0024 x" {
		t.Fatalf("filter = %q, want %q", m.filter, "éFA0024 x")
	}
}

func TestExploreReaderListingKeepsSelectionVisible(t *testing.T) {
	m := replayModel(intexplore.ProtocolOff)
	m.height = 12
	m.reading = true
	m.reader = exploreReader{title: "docs", listing: true}
	for i := range 40 {
		m.reader.entries = append(m.reader.entries, readerEntry{name: fmt.Sprintf("doc-%02d.md", i)})
	}
	visible := func(m exploreModel) string {
		for _, line := range strings.Split(m.renderReader(), "\n") {
			if strings.HasPrefix(line, "> ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "> "))
			}
		}
		return ""
	}
	for range 25 {
		m, _ = press(t, m, runes("j"))
	}
	if got := visible(m); got != "doc-25.md" {
		t.Fatalf("after 25 downs the visible selection = %q, want doc-25.md", got)
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.reader.entry <= 25 {
		t.Fatalf("ctrl+d left the selection at %d", m.reader.entry)
	}
	if got, want := visible(m), m.reader.entries[m.reader.entry].name; got != want {
		t.Fatalf("after ctrl+d the visible selection = %q, want %q", got, want)
	}
	for range 40 {
		m, _ = press(t, m, runes("k"))
	}
	if got := visible(m); got != "doc-00.md" {
		t.Fatalf("after moving back up the visible selection = %q, want doc-00.md", got)
	}
}

func TestExploreResizeReflowsReader(t *testing.T) {
	m := replayModel(intexplore.ProtocolOff)
	m.width = 120
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.reading {
		t.Fatal("enter did not open the reader")
	}
	m, _ = press(t, m, tea.WindowSizeMsg{Width: 44, Height: 30})
	limit := m.contentWidth()
	for _, line := range m.reader.lines {
		if w := lipgloss.Width(line); w > limit {
			t.Fatalf("reader line is %d wide after narrowing to %d: %q", w, limit, line)
		}
	}
}

// A pty whose size was never set reports 0x0. Laid out for that, the feed was
// one line saying the terminal is too small, with a camp's worth of rows
// behind it.
func TestExploreIgnoresASizeWithNoCells(t *testing.T) {
	m := screenFeed(t, 80, 24, intexplore.ProtocolKitty, 6)
	for _, size := range []tea.WindowSizeMsg{{}, {Width: 0, Height: 24}, {Width: 80, Height: 0}, {Width: -1, Height: -1}} {
		got, cmd := press(t, m, size)
		if got.width != 80 || got.height != 24 || got.sized || cmd != nil {
			t.Fatalf("size %dx%d: model is %dx%d sized=%v cmd=%v; want it left at 80x24", size.Width, size.Height, got.width, got.height, got.sized, cmd != nil)
		}
		if view := got.View(); strings.Contains(view, "too small") || !strings.Contains(view, "Item 00") {
			t.Fatalf("size %dx%d: the feed is not drawn:\n%s", size.Width, size.Height, view)
		}
	}
	// The first size with cells is still the first: its replay is asked for.
	m, _ = press(t, m, tea.WindowSizeMsg{})
	if _, cmd := press(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}); cmd == nil {
		t.Fatal("the first real size did not ask for the focused replay")
	}
}

func TestExploreResizeSchedulesSkippedReplay(t *testing.T) {
	m := replayModel(intexplore.ProtocolKitty)
	m.width, m.height = 50, 12
	m.poster, m.mediaFor = nil, ""
	m, _ = press(t, m, tea.WindowSizeMsg{Width: 50, Height: 12})
	gen := m.loadGen
	m, cmd := press(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd == nil || m.loadGen == gen {
		t.Fatal("enlarging the terminal did not schedule the replay it skipped")
	}
}

func TestExplorePauseInvalidatesPendingTick(t *testing.T) {
	m := replayModel(intexplore.ProtocolKitty)
	m.frames = [][]byte{{1}, {2}, {3}}
	m.delays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	m.playing = true
	pending := m.tick()()
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m, resumed := press(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if !m.playing || resumed == nil {
		t.Fatal("space did not resume playback")
	}
	frame := m.frame
	m, cmd := press(t, m, pending)
	if cmd != nil || m.frame != frame {
		t.Fatal("a tick scheduled before the pause still advanced playback")
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

func TestExploreJumpPreservesUTF8Paths(t *testing.T) {
	rel := "festivals/.dungeon/completed/café-FA0001"
	got := joinAbs("/camp", rel)
	if want := filepath.Join("/camp", filepath.FromSlash(rel)); got != want {
		t.Fatalf("joinAbs = %q, want %q", got, want)
	}
	if strings.ContainsRune(got, 0) {
		t.Fatalf("joinAbs inserted NUL bytes: %q", got)
	}
}

func TestExploreMarkdownHonorsPlainOutput(t *testing.T) {
	doc := "# Festival goal\n\nShip **every** platform with `code` and a [link](https://example.com).\n"
	plain := strings.Join(renderExploreMarkdown(doc, 60, true, ""), "\n")
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("plain Markdown carries escape sequences: %q", plain)
	}
	if !strings.Contains(plain, "every") || !strings.Contains(plain, "Festival goal") {
		t.Fatalf("plain Markdown lost its text: %q", plain)
	}
	if styled := strings.Join(renderExploreMarkdown(doc, 60, false, ""), "\n"); !strings.Contains(styled, "\x1b[") {
		t.Fatal("styled Markdown carries no styling; the plain check proves nothing")
	}
}

func TestExploreFocusChangeCancelsReplayDecode(t *testing.T) {
	m := replayModel(intexplore.ProtocolKitty)
	m.index.Items[1].Replay = m.index.Items[1].Path + "/replay.gif"
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	started := make(chan context.Context, 4)
	m.decode = func(ctx context.Context, _ string, _, _ int) (intexplore.Frames, error) {
		started <- ctx
		<-ctx.Done()
		return intexplore.Frames{}, ctx.Err()
	}
	run := func(cmd tea.Cmd) <-chan tea.Msg {
		out := make(chan tea.Msg, 1)
		go func() { out <- cmd() }()
		return out
	}
	await := func(what string) context.Context {
		select {
		case ctx := <-started:
			return ctx
		case <-time.After(2 * time.Second):
			t.Fatalf("%s decode never started", what)
			return nil
		}
	}

	m, cmd := press(t, m, exploreDecode{gen: m.loadGen})
	if cmd == nil {
		t.Fatal("no decode for the focused replay")
	}
	firstDone := run(cmd)
	first := await("first")

	m, _ = press(t, m, runes("j"))
	if !errors.Is(first.Err(), context.Canceled) {
		t.Fatal("moving focus left the first decode running")
	}
	select {
	case msg := <-firstDone:
		m, _ = press(t, m, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled decode did not return")
	}
	if row := m.statusRow(); row.failed {
		t.Fatalf("a cancelled decode reported %q", row.text)
	}

	m, cmd = press(t, m, exploreDecode{gen: m.loadGen})
	secondDone := run(cmd)
	second := await("second")
	if second.Err() != nil {
		t.Fatal("the new focus started with a cancelled decode")
	}
	m, _ = press(t, m, runes("q"))
	if !errors.Is(second.Err(), context.Canceled) {
		t.Fatal("quitting left the decode running")
	}
	<-secondDone
}
