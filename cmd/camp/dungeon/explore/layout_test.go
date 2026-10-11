package explore

import (
	"strings"
	"testing"

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

func TestExploreReaderStripsFrontmatter(t *testing.T) {
	body := readerBody{
		text:     "---\nfest_type: festival\nfest_id: FA0024\nfest_name: festival-activity-desktop\nfest_status: dungeon/completed\n---\n\n# Festival Goal\n\nShip the desktop app.\n",
		markdown: true,
	}
	got := strings.Join(body.render(80, true, ""), "\n")
	for _, leak := range []string{"fest_type", "fest_id", "fest_status", "--------"} {
		if strings.Contains(got, leak) {
			t.Fatalf("reader shows frontmatter %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "Ship the desktop app.") {
		t.Fatalf("reader lost the goal body:\n%s", got)
	}
	plain := readerBody{text: "# No frontmatter\n\n---\n\nA rule above stays.\n", markdown: true}
	if got := strings.Join(plain.render(80, true, ""), "\n"); !strings.Contains(got, "No frontmatter") || !strings.Contains(got, "A rule above stays.") {
		t.Fatalf("document without frontmatter changed:\n%s", got)
	}
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

func TestExploreReaderWrapsAtFullWidth(t *testing.T) {
	m := replayModel(intexplore.ProtocolOff)
	m.width = 120
	m.index.Items[0].Summary = strings.Repeat("streak recap ", 30)
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	widest := 0
	for _, line := range m.reader.lines {
		widest = max(widest, lipgloss.Width(line))
	}
	if widest <= m.width/2 || widest > m.width {
		t.Fatalf("reader wraps at %d columns on a %d-column terminal", widest, m.width)
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
