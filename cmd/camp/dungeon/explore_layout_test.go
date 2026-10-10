package dungeon

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

const longFestID = "add-weekly-streak-recap-20260902-091500"

func longIDModel(width, height int) exploreModel {
	m := exploreModel{
		width:    width,
		height:   height,
		protocol: explore.ProtocolOff,
		query:    explore.Query{Status: "finished", Dungeon: explore.LensAll},
		index: explore.Index{
			Dungeons: []explore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
			Items: []explore.Item{{
				DoneDate: "2026-09-02", DateSource: explore.DateBucket, Status: "completed",
				DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: explore.KindFestival,
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
	got := strings.Join(body.render(80, true), "\n")
	for _, leak := range []string{"fest_type", "fest_id", "fest_status", "--------"} {
		if strings.Contains(got, leak) {
			t.Fatalf("reader shows frontmatter %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "Ship the desktop app.") {
		t.Fatalf("reader lost the goal body:\n%s", got)
	}
	plain := readerBody{text: "# No frontmatter\n\n---\n\nA rule above stays.\n", markdown: true}
	if got := strings.Join(plain.render(80, true), "\n"); !strings.Contains(got, "No frontmatter") || !strings.Contains(got, "A rule above stays.") {
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
		if last := lines[len(lines)-1]; last != m.renderHelp() {
			t.Fatalf("%s last row = %q, want the help line", name, last)
		}
	}
}

func TestExploreReaderWrapsAtFullWidth(t *testing.T) {
	m := replayModel(explore.ProtocolOff)
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
		if help := m.renderHelp(); lipgloss.Width(help) > width || !strings.HasSuffix(help, "q quit") {
			t.Fatalf("full help at %d columns = %q", width, help)
		}
		reading, _ := press(t, longIDModel(width, 24), tea.KeyMsg{Type: tea.KeyEnter})
		if help := reading.renderHelp(); lipgloss.Width(help) > width || !strings.HasSuffix(help, "esc back") {
			t.Fatalf("reader help at %d columns = %q", width, help)
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
