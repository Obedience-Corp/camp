package explore

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

const longFestID = "add-weekly-streak-recap-20260902-091500"

func longIDModel(width, height int) exploreModel {
	m := exploreModel{
		width:    width,
		height:   height,
		protocol: intexplore.ProtocolOff,
		query:    intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll},
		index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
			Items: []intexplore.Item{{
				DoneDate: "2026-09-02", DateSource: intexplore.DateBucket, Status: "completed",
				DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival,
				Title: longFestID, ID: longFestID, Summary: "Recap the weekly streak for every member of the camp",
				Path: "festivals/.dungeon/completed/2026-09-02/" + longFestID, IsDir: true,
			}},
		},
	}
	if err := m.applyQuery(); err != nil {
		panic(err)
	}
	return m
}

func viewLines(m exploreModel) []string {
	return strings.Split(m.View(), "\n")
}

func TestExploreWideLayoutKeepsGutter(t *testing.T) {
	m := longIDModel(100, 30)
	leftW := m.width / 2
	lines := viewLines(m)
	checked := 0
	for _, line := range lines[1 : len(lines)-1] {
		runes := []rune(line)
		if len(runes) <= leftW {
			continue
		}
		checked++
		if gutter := string(runes[leftW-exploreGutter : leftW]); strings.TrimSpace(gutter) != "" {
			t.Fatalf("left pane runs into the card: %q", line)
		}
	}
	if checked == 0 {
		t.Fatal("no two-pane rows rendered")
	}
}

func TestExploreHelpLineSitsOnLastRow(t *testing.T) {
	feed := longIDModel(70, 24)
	reading, _ := press(t, feed, tea.KeyMsg{Type: tea.KeyEnter})
	if !reading.reading {
		t.Fatal("enter did not open the reader")
	}
	for name, m := range map[string]exploreModel{"feed": feed, "reader": reading} {
		lines := viewLines(m)
		if len(lines) != m.height {
			t.Fatalf("%s view has %d rows, want %d", name, len(lines), m.height)
		}
		if last := lines[len(lines)-1]; last != strings.Join(m.helpLines(), "\n") {
			t.Fatalf("%s last row = %q, want the help line", name, last)
		}
	}
}

func TestExploreNarrowHelpKeepsQuit(t *testing.T) {
	for _, width := range []int{64, 40, 24} {
		m := longIDModel(width, 24)
		lines := viewLines(m)
		help := lines[len(lines)-1]
		if lipgloss.Width(help) > width || !strings.HasSuffix(help, "q quit") {
			t.Fatalf("help at %d columns = %q", width, help)
		}
		m.help = true
		if help := strings.Join(m.helpLines(), "\n"); lipgloss.Width(help) > width || !strings.HasSuffix(help, "q quit") {
			t.Fatalf("full help at %d columns = %q", width, help)
		}
		reading, _ := press(t, longIDModel(width, 24), tea.KeyMsg{Type: tea.KeyEnter})
		if help := strings.Join(reading.helpLines(), "\n"); lipgloss.Width(help) > width || !strings.HasSuffix(help, "esc back") {
			t.Fatalf("reader help at %d columns = %q", width, help)
		}
	}
}

func TestExploreViewShowsScanAndItemWarnings(t *testing.T) {
	m := exploreModel{
		width:    100,
		height:   30,
		protocol: intexplore.ProtocolOff,
		query:    intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll},
		index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
			Items: []intexplore.Item{
				{
					DoneDate: "2026-10-05", DateSource: intexplore.DateBucket, Status: "completed",
					DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival,
					Title: "festival-activity-desktop", ID: "FA0024", Path: "festivals/.dungeon/completed/clean", IsDir: true,
				},
				{
					DoneDate: "2026-09-02", DateSource: intexplore.DateBucket, Status: "completed",
					DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival,
					Title: "broken-festival", Path: "festivals/.dungeon/completed/broken", IsDir: true,
				},
			},
		},
	}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	clean := m.View()
	if len(viewLines(m)) != m.height {
		t.Fatalf("clean view has %d rows, want %d", len(viewLines(m)), m.height)
	}
	m.index.Warnings = []string{" ", ""}
	m.visible.Items[0].Warning = "  "
	m.visible.Items[1].Warning = "\t"
	if m.View() != clean {
		t.Fatal("blank warnings changed the layout")
	}

	const scan = "festivals/.dungeon: unreadable directory"
	const itemWarn = "fest.yaml: mapping values are not allowed"
	m.index.Warnings = []string{scan}
	m.visible.Items[1].Warning = itemWarn
	focused := m.View()
	if !strings.Contains(focused, scan) {
		t.Fatalf("scan warning missing:\n%s", focused)
	}
	if strings.Contains(focused, itemWarn) {
		t.Fatalf("unfocused item warning is showing:\n%s", focused)
	}
	m.cursor = 1
	focused = m.View()
	if !strings.Contains(focused, scan) || !strings.Contains(focused, itemWarn) {
		t.Fatalf("focused warnings missing:\n%s", focused)
	}
	if len(viewLines(m)) != m.height {
		t.Fatalf("warnings changed the view height to %d", len(viewLines(m)))
	}

	body := m.bodyHeight()
	m.index.Warnings = []string{scan, "docs/.dungeon: permission denied", "workflow/.dungeon: permission denied"}
	many := m.View()
	if !strings.Contains(many, "3 scan warnings, first: "+scan) || strings.Contains(many, "docs/.dungeon") {
		t.Fatalf("scan warnings were not collapsed to one row:\n%s", many)
	}
	if m.bodyHeight() != body {
		t.Fatalf("more scan warnings shrank the body from %d to %d rows", body, m.bodyHeight())
	}
}

func feedModel(t *testing.T, items []intexplore.Item, query intexplore.Query) exploreModel {
	t.Helper()
	m := exploreModel{
		width:    80,
		height:   24,
		protocol: intexplore.ProtocolOff,
		query:    query,
		index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{
				{Path: "festivals/.dungeon", Label: "Festivals"},
				{Path: "notes/.dungeon", Label: "Notes"},
			},
			Items: items,
		},
	}
	if err := m.applyQuery(); err != nil {
		t.Fatalf("apply: %v", err)
	}
	return m
}

func TestExploreEmptyFinishedLens(t *testing.T) {
	m := feedModel(t, []intexplore.Item{{
		DoneDate: "2026-10-05", Status: "holding",
		DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon",
		Title: "later", Path: "festivals/.dungeon/later.md",
	}}, intexplore.Query{})
	if len(m.visible.Items) != 0 {
		t.Fatalf("finished lens kept holding work: %+v", m.visible.Items)
	}
	view := m.View()
	if !strings.Contains(view, "No finished work in this camp.") {
		t.Fatalf("view = %s", view)
	}
}

func TestExploreEmptyStateUsesFilters(t *testing.T) {
	items := []intexplore.Item{
		{
			DoneDate: "2026-10-05", Status: "completed",
			DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon",
			Title: "shipped", ID: "SHIP-1", Path: "festivals/.dungeon/completed/shipped",
		},
		{
			DoneDate: "2026-04-01", Status: "reviewed",
			DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon",
			Title: "notes", Path: "festivals/.dungeon/reviewed/notes",
		},
	}
	cases := []struct {
		name  string
		query intexplore.Query
	}{
		{name: "archived", query: intexplore.Query{Status: "archived", Dungeon: intexplore.LensAll}},
		{name: "holding", query: intexplore.Query{Status: "holding", Dungeon: intexplore.LensAll}},
		{name: "done", query: intexplore.Query{Status: "done", Dungeon: intexplore.LensAll}},
		{name: "custom status", query: intexplore.Query{Status: "reviewed", Dungeon: intexplore.LensAll, Text: "missing"}},
		{name: "text", query: intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll, Text: "missing"}},
		{name: "since", query: intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll, Since: "2027-01-01"}},
		{name: "until", query: intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll, Until: "2020-01-01"}},
		{name: "dungeon", query: intexplore.Query{Status: "finished", Dungeon: "Notes"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := feedModel(t, items, tc.query)
			if len(m.visible.Items) != 0 {
				t.Fatalf("expected an empty feed, got %+v", m.visible.Items)
			}
			view := m.View()
			if strings.Contains(view, "No finished work") {
				t.Fatalf("empty %s feed claimed there is no finished work:\n%s", tc.name, view)
			}
			if !strings.Contains(view, "No items match the current filters.") {
				t.Fatalf("empty %s feed:\n%s", tc.name, view)
			}
		})
	}
}

func TestExploreEmptyStatesSayWhatHappened(t *testing.T) {
	query := intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll}
	// A first load that fails with no cache knows nothing about the camp.
	m := exploreModel{width: 80, height: 24, loading: true, protocol: intexplore.ProtocolOff, query: query}
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
	m, _ = press(t, m, exploreLoaded{gen: m.indexGen, index: intexplore.Index{BuiltAt: time.Now()}, changed: true})
	if view := viewText(m.View()); !strings.Contains(view, "No dungeons in this camp.") || strings.Contains(view, "could not be read") {
		t.Fatalf("an empty camp after a successful load:\n%s", view)
	}

	// No empty-state row is cut mid-word by the renderer.
	states := map[string]exploreModel{
		"no dungeons": {query: query},
		"load failed": {query: query, loadFailed: true},
		"first load":  {query: query, loading: true},
		"no finished work": {query: query, index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
		}},
		"no match": {query: intexplore.Query{Status: "archived", Dungeon: intexplore.LensAll}, index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
		}},
	}
	for name, state := range states {
		for _, width := range []int{20, 24} {
			state.width, state.height, state.protocol = width, 6, intexplore.ProtocolOff
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

func TestExploreLongIDKeepsDungeonLabel(t *testing.T) {
	for _, width := range []int{64, 100} {
		m := longIDModel(width, 30)
		var row string
		for _, line := range viewLines(m) {
			if strings.HasPrefix(line, "> ") {
				row = line
				break
			}
		}
		if !strings.Contains(row, "Festivals") {
			t.Fatalf("focused row at %d columns lost its dungeon label: %q", width, row)
		}
	}
}

// In the narrow layout the replay sits between the focused row and its card.
// The card holds the only copy of the status, the date, and the path.
func TestExploreNarrowCardStaysVisibleUnderTheReplay(t *testing.T) {
	for _, size := range [][2]int{{80, 22}, {80, 24}, {80, 28}, {60, 24}, {87, 30}} {
		for _, status := range []string{"", "copied"} {
			m := screenFeed(t, size[0], size[1], intexplore.ProtocolKitty, 60)
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

func TestExploreLayoutMatrix(t *testing.T) {
	sizes := [][2]int{
		{20, 4}, {20, 5}, {21, 6}, {40, 10}, {40, 30}, {59, 22}, {60, 16}, {60, 21}, {60, 22}, {80, 12}, {80, 21}, {80, 22}, {80, 24},
		{87, 30}, {88, 15}, {88, 16}, {88, 17}, {88, 18}, {100, 24}, {100, 29}, {100, 30}, {120, 16}, {120, 40}, {200, 16}, {200, 60},
	}
	doc := readerBody{markdown: true, text: strings.Repeat("# Heading\n\nSome **markdown** that is long enough to wrap on a narrow terminal.\n\n", 30)}
	seen := map[string]bool{}
	report := func(label string, m exploreModel) {
		for _, problem := range checkLayout(m) {
			// One line per kind of problem and mode keeps a broken layout readable.
			key := label[strings.LastIndex(label, " ")+1:] + ": " + problem
			if len(key) > 60 {
				key = key[:60]
			}
			if !seen[key] {
				seen[key] = true
				t.Errorf("%s: %s", label, problem)
			}
		}
	}
	for _, size := range sizes {
		for _, protocol := range []string{intexplore.ProtocolOff, intexplore.ProtocolKitty, intexplore.ProtocolITerm} {
			for _, n := range []int{0, 1, 3, 40} {
				for _, notices := range []int{0, 1, 2} {
					for _, status := range []string{"", "copied"} {
						base := emojiTitles(t, screenFeed(t, size[0], size[1], protocol, n))
						if notices >= 1 {
							base.index.Warnings = []string{"festivals/.dungeon/archived: permission denied"}
						}
						if notices >= 2 {
							for i := range base.visible.Items {
								base.visible.Items[i].Warning = "fest.yaml: mapping values are not allowed"
							}
						}
						base.status = status
						cursors := []int{0}
						if rows := len(base.visible.Items); rows > 1 {
							cursors = []int{0, rows / 2, rows - 1}
						}
						for _, cursor := range cursors {
							m := base
							m.cursor = cursor
							if item, ok := m.focused(); ok && item.Replay != "" {
								w, h := m.stagePixels()
								m.poster, m.mediaFor = posterFor(item.Path, w, h), item.Path
							}
							label := fmt.Sprintf("%dx%d %s rows=%d notices=%d status=%q cursor=%d", size[0], size[1], protocol, n, notices, status, cursor)
							report(label+" feed", m)

							help := m
							help.help = true
							report(label+" help", help)

							filter := m
							filter.filtering = true
							filter.filter = strings.Repeat("query ", 30)
							report(label+" filter", filter)

							loading := m
							loading.loading = true
							report(label+" loading", loading)

							if _, ok := m.focused(); !ok {
								continue
							}
							reader := m
							reader.reading = true
							reader.reader = exploreReader{title: "doc", item: m.visible.Items[cursor], body: doc}
							reader.reader.lines = doc.render(reader.contentWidth(), true, "")
							report(label+" reader", reader)
							reader.reader.offset = len(reader.reader.lines)
							report(label+" reader-end", reader)

							listing := m
							listing.reading = true
							listing.reader = exploreReader{title: "docs", listing: true, item: m.visible.Items[cursor]}
							for i := range 50 {
								listing.reader.entries = append(listing.reader.entries, readerEntry{name: fmt.Sprintf("a-long-document-name-that-keeps-going-and-going-%02d.md", i)})
							}
							listing.reader.entry = 30
							report(label+" listing", listing)
						}
					}
				}
			}
		}
		empty := exploreModel{width: size[0], height: size[1], protocol: intexplore.ProtocolOff, query: intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll}}
		_ = empty.applyQuery()
		label := fmt.Sprintf("%dx%d", size[0], size[1])
		report(label+" no-dungeons", empty)
		empty.loading = true
		report(label+" first-load", empty)
		empty.loading, empty.loadFailed = false, true
		empty.setStatus(exploreLoadStatus, "open /camp: permission denied", true)
		report(label+" load-failed", empty)
	}
}
