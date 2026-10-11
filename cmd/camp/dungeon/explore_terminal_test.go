package dungeon

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

// screenScript is one walk through the explorer. After every step the grid
// must show the replay exactly where the model asked for it and nowhere else.
type screenScript struct {
	name     string
	w, h, n  int
	warnings []string
	start    int // rows to move down before the script begins
	steps    []screenAction
}

type screenAction struct {
	name string
	do   func(s *exploreScreen) exploreModel
}

func pressKey(name string) screenAction {
	return screenAction{name: name, do: func(s *exploreScreen) exploreModel { return s.key(name) }}
}

// pressBare sends a key and stops at the frame before the replay arrives.
func pressBare(name string) screenAction {
	return screenAction{name: name + " (before the replay)", do: func(s *exploreScreen) exploreModel { return s.send(screenKey(name)) }}
}

func replayArrives() screenAction {
	return screenAction{name: "replay arrives", do: func(s *exploreScreen) exploreModel { return s.send(screenSettle{}) }}
}

func pressKeys(names ...string) []screenAction {
	out := make([]screenAction, len(names))
	for i, name := range names {
		out[i] = pressKey(name)
	}
	return out
}

// refreshWithout delivers a refresh whose index has lost the focused row.
func refreshWithout() screenAction {
	return screenAction{name: "refresh drops the focused row", do: func(s *exploreScreen) exploreModel {
		m := s.send(nil)
		focused, _ := m.focused()
		idx := m.index
		idx.Items = nil
		for _, item := range m.index.Items {
			if item.Path != focused.Path {
				idx.Items = append(idx.Items, item)
			}
		}
		s.send(exploreLoaded{gen: m.indexGen, index: idx, changed: true})
		return s.send(screenSettle{})
	}}
}

func refreshEmpty() screenAction {
	return screenAction{name: "refresh finds no rows", do: func(s *exploreScreen) exploreModel {
		m := s.send(nil)
		idx := m.index
		idx.Items = nil
		return s.send(exploreLoaded{gen: m.indexGen, index: idx, changed: true})
	}}
}

// The four ways an iTerm replay went wrong, each on the layout and size it
// was found at. Kitty runs the same walks: it never had the defects and its
// output must stay correct.
var screenScripts = []screenScript{
	{
		// A status row shifts the feed window by a row. Every body row is
		// rewritten and erased to its end, which takes the image with it.
		name: "wide: a status row repaints the rows under the replay",
		w:    120, h: 40, n: 60, start: 21,
		steps: pressKeys("g", "j", "j", "g", "r", "?", "?", "k"),
	},
	{
		name: "wide at 100x24: a status row repaints the rows under the replay",
		w:    100, h: 24, n: 60, start: 21,
		steps: pressKeys("g", "j", "j", "g", "r"),
	},
	{
		// Reservation rows are blank before and after, so nothing repainted them.
		name: "narrow: the reader opens over the replay",
		w:    80, h: 24, n: 60, start: 21,
		steps: pressKeys("enter", "esc", "enter", "j", "esc"),
	},
	{
		name: "narrow: the lens empties the feed under the replay",
		w:    80, h: 30, n: 4,
		steps: pressKeys("s", "s", "s", "s", "s", "s", "s", "s"),
	},
	{
		name: "narrow: a refresh removes the row under the replay",
		w:    80, h: 24, n: 60, start: 21,
		steps: []screenAction{refreshWithout(), pressKey("j"), refreshEmpty()},
	},
	{
		// The placement moves up a row and used to leave its old last row.
		name: "narrow: a status row moves the replay by one row",
		w:    80, h: 24, n: 60, start: 21,
		steps: pressKeys("g", "j", "j", "g", "k", "g", "r"),
	},
	{
		name: "narrow: the filter prompt hides a notice row above the replay",
		w:    80, h: 24, n: 60, start: 21,
		warnings: []string{"festivals/.dungeon/archived: permission denied"},
		steps:    pressKeys("/", "esc", "g", "/", "esc", "j"),
	},
	{
		name: "wide: focus moves to a row without a replay",
		w:    120, h: 40, n: 6,
		steps: []screenAction{
			pressKey("j"), pressKey("j"), pressKey("k"), pressKey("j"), pressKey("j"),
			pressBare("k"), replayArrives(), pressBare("k"), replayArrives(),
		},
	},
	{
		name: "wide: the reader, the filter, and the lens over a replay",
		w:    120, h: 40, n: 60, start: 21,
		steps: pressKeys("enter", "esc", "/", "esc", "s", "s", "[", "]"),
	},
}

func TestExploreReplayOnTheTerminal(t *testing.T) {
	for _, protocol := range []string{explore.ProtocolITerm, explore.ProtocolKitty} {
		for _, script := range screenScripts {
			t.Run(protocol+"/"+script.name, func(t *testing.T) {
				m := screenFeed(t, script.w, script.h, protocol, script.n)
				m.index.Warnings = script.warnings
				m.cursor = min(script.start, len(m.visible.Items)-1)
				for m.visible.Items[m.cursor].Replay == "" {
					m.cursor++
				}
				s := openExploreScreen(t, m)
				check := func(step string, m exploreModel) {
					t.Helper()
					for _, problem := range checkScreen(s.grid, m) {
						t.Errorf("after %s: %s\n%s", step, problem, s.grid)
					}
				}
				first := s.send(screenSettle{})
				if _, _, _, _, ok := imageBox(first.View()); !ok {
					t.Fatal("the walk does not start on a drawn replay, so it checks nothing")
				}
				check("the first replay", first)
				for _, step := range script.steps {
					check(step.name, step.do(s))
					if t.Failed() {
						return
					}
				}

				// The same walk with no render forced between steps: the
				// renderer skips whatever frames its timer misses, and the
				// screen must still end where the model does.
				s = openExploreScreen(t, m)
				s.send(screenSettle{})
				s.burst = true
				for _, step := range script.steps {
					step.do(s)
				}
				s.burst = false
				check("the whole walk in one burst", s.send(nil))
			})
		}
	}
}

// Kitty's image is a layer of its own: erasing text under it does nothing,
// and it has a delete command. The iTerm row marks and row digest must not
// reach it.
func TestExploreKittyViewCarriesNoITermRepaintMarks(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		m := screenFeed(t, size[0], size[1], explore.ProtocolKitty, 6)
		m.poster, m.mediaFor = posterFor(m.visible.Items[0].Path, 64, 64), m.visible.Items[0].Path
		view := m.View()
		if !explore.HasKittyImage(view) {
			t.Fatalf("%dx%d: no Kitty image in the view", size[0], size[1])
		}
		if strings.Contains(view, "\x1b]1337") || viewCSIErase.MatchString(view) {
			t.Fatalf("%dx%d: the Kitty view carries iTerm sequences", size[0], size[1])
		}
	}
}

// The image row must change when, and only when, a row under the image does.
func TestExploreITermImageRowFollowsCoveredRows(t *testing.T) {
	m := screenFeed(t, 120, 40, explore.ProtocolITerm, 60)
	m.cursor = 21
	item, _ := m.focused()
	m.poster, m.mediaFor = posterFor(item.Path, 384, 192), item.Path
	last := func(m exploreModel) string {
		view := m.View()
		return view[strings.LastIndex(view, "\n")+1:]
	}
	before := last(m)
	if again := last(m); again != before {
		t.Fatal("an unchanged frame changed the image row, so the replay would flicker")
	}
	// The help text is the same; only the feed rows under the image moved.
	shifted := m
	shifted.setStatus(exploreActionStatus, "copied", false)
	if viewText(last(shifted)) != viewText(before) {
		t.Fatal("fixture: the help text changed, so the row would repaint anyway")
	}
	if last(shifted) == before {
		t.Fatal("rows under the image changed and the image row did not: iTerm would lose the image")
	}
}

func TestExploreReaderTabsOnTheTerminal(t *testing.T) {
	doc := "# Design\n\nThe loader wraps errors:\n\n```go\nfunc load(ctx context.Context) error {\n" +
		"\tfor _, item := range items {\n\t\tif err := read(ctx, item); err != nil {\n" +
		"\t\t\treturn camperrors.Wrap(err, \"reading festival metadata\")\n\t\t}\n\t}\n\treturn nil\n}\n```\n\n" +
		"name\tstatus\tdate\tpath\towner\tsize\tkind\tlabel\tnote\tmore\tand\tmore\n\nDone.\n"
	bodies := map[string]readerBody{
		"markdown": {text: doc, markdown: true},
		"plain":    {text: doc},
	}
	for name, body := range bodies {
		for _, plain := range []bool{true, false} {
			m := screenFeed(t, 80, 24, explore.ProtocolOff, 3)
			m.plain = plain
			m.reading = true
			m.reader = exploreReader{title: "design.md", item: m.visible.Items[0], body: body}
			m.reader.lines = body.render(m.contentWidth(), plain)
			for _, line := range m.reader.lines {
				if strings.Contains(line, "\t") {
					t.Fatalf("%s plain=%v: a raw tab reached the reader line %q", name, plain, line)
				}
				if w := lipgloss.Width(line); w > m.contentWidth() {
					t.Fatalf("%s plain=%v: line is %d columns in a %d-column reader: %q", name, plain, w, m.contentWidth(), line)
				}
			}
			s := openExploreScreen(t, m)
			m = s.send(nil)
			if s.grid.tabs > 0 || s.grid.wrapped > 0 || s.grid.scrolled > 0 {
				t.Fatalf("%s plain=%v: %d tabs sent, %d rows wrapped, %d scrolls\n%s", name, plain, s.grid.tabs, s.grid.wrapped, s.grid.scrolled, s.grid)
			}
			if got := s.grid.rowText(0); got != "design.md" {
				t.Fatalf("%s plain=%v: header row = %q\n%s", name, plain, got, s.grid)
			}
			if got := s.grid.rowText(m.height - 1); !strings.Contains(got, "esc back") {
				t.Fatalf("%s plain=%v: help row = %q\n%s", name, plain, got, s.grid)
			}
			screen := s.grid.String()
			for _, want := range []string{"Design", "return camperrors.Wrap(err,", "status"} {
				if !strings.Contains(screen, want) {
					t.Fatalf("%s plain=%v: %q is not on screen\n%s", name, plain, want, screen)
				}
			}
		}
	}
}

func TestExploreExpandTabs(t *testing.T) {
	for in, want := range map[string]string{
		"no tabs":       "no tabs",
		"\tx":           "    x",
		"a\tb":          "a   b",
		"abcd\te":       "abcd    e",
		"界\tx":          "界  x",
		"a\n\tb\n\t\tc": "a\n    b\n        c",
	} {
		if got := expandTabs(in); got != want {
			t.Errorf("expandTabs(%q) = %q, want %q", in, got, want)
		}
	}
	// Glamour is never handed a tab, but its output is filtered all the same:
	// a tab becomes the one column it was measured as beside its neighbors.
	if got := safeReaderOutput("a\tb\x1b[31m\tc\x1b[0m", false); got != "a b\x1b[31m c\x1b[0m" {
		t.Fatalf("reader filter on tabs = %q", got)
	}
}

// A styled token longer than the reader is hard-wrapped. Each piece must
// open and close its own style, or the renderer's erase paints the style to
// the margin and the rows below inherit it.
func TestExploreReaderClosesStylesAtHardBreaks(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	doc := "# Notes\n\nSee `" + strings.Repeat("internal/dungeon/explore/", 8) + "replay.go` and https://example.com/" +
		strings.Repeat("segment/", 16) + "end for the rest.\n"
	lines := renderExploreMarkdown(doc, 38, false)
	continued := 0
	for i, line := range lines {
		open := openStyleAtEnd(line)
		if open != "" {
			t.Fatalf("line %d ends with the style %q still open: %q", i, open, line)
		}
		text := viewText(line)
		if i > 0 && strings.HasPrefix(text, "ngeon/explore/") || strings.HasPrefix(text, "segment/") {
			continued++
			if !strings.HasPrefix(line, "\x1b[") {
				t.Fatalf("continuation line %d lost its style: %q", i, line)
			}
		}
		if w := lipgloss.Width(line); w > 38 {
			t.Fatalf("line %d is %d columns wide: %q", i, w, line)
		}
	}
	if continued == 0 {
		t.Fatalf("fixture has no hard-wrapped styled token:\n%s", strings.Join(lines, "\n"))
	}

	// On the terminal: with the styled token cut by the bottom of the body,
	// nothing is erased while a style is open.
	m := screenFeed(t, 40, 12, explore.ProtocolOff, 3)
	m.styles = newExploreStyles(false)
	m.reading = true
	m.reader = exploreReader{title: "notes.md", item: m.visible.Items[0], body: readerBody{text: doc, markdown: true}}
	m.reader.lines = m.reader.body.render(m.contentWidth(), false)
	s := openExploreScreen(t, m)
	for range len(m.reader.lines) {
		s.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if s.grid.styledErases > 0 {
		t.Fatalf("the renderer erased %d times with a document style still open\n%s", s.grid.styledErases, s.grid)
	}
	if s.grid.wrapped > 0 || s.grid.scrolled > 0 {
		t.Fatalf("reader wrapped %d rows and scrolled %d times", s.grid.wrapped, s.grid.scrolled)
	}
}

// openStyleAtEnd replays a line's SGR sequences and returns what is still
// set when the line ends.
func openStyleAtEnd(line string) string {
	open := ""
	for _, seq := range viewCSI.FindAllString(line, -1) {
		if !strings.HasSuffix(seq, "m") {
			continue
		}
		switch seq[2 : len(seq)-1] {
		case "", "0":
			open = ""
		default:
			open += seq
		}
	}
	return open
}

// The frame between a key press and the next must not depend on timing. This
// guards the harness itself: a step is on the grid when send returns.
func TestExploreScreenStepsAreFlushedInOrder(t *testing.T) {
	m := screenFeed(t, 80, 24, explore.ProtocolOff, 10)
	s := openExploreScreen(t, m)
	start := time.Now()
	for i := range 5 {
		got := s.key("j")
		if got.cursor != i+1 {
			t.Fatalf("cursor = %d after %d downs", got.cursor, i+1)
		}
		focused, _ := got.focused()
		if !strings.Contains(s.grid.String(), "> "+focused.Title) {
			t.Fatalf("the grid does not show %q focused\n%s", focused.Title, s.grid)
		}
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("five steps took more than five seconds")
	}
}
