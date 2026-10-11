package explore

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

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

func TestExploreReaderRejectsRenderedCommands(t *testing.T) {
	payloads := []string{
		"&#x1b;]52;c;ZXZpbA==&#7;",
		"&#27;]0;spoofed title&#27;\\",
		"&#27;[2J&#27;[H",
		"&#27;Pmalicious&#27;\\",
		"&#27;_Gmalicious&#27;\\",
		"&#x9b;2J",
		"&#x1b;]8;;https://example.com&#7;link&#x1b;]8;;&#7;",
		"&#27;[31&#27;]52;c;evil&#7;m",
	}
	sgr := regexp.MustCompile(`\x1b\[[0-9;:]*m`)
	for _, payload := range payloads {
		for _, plain := range []bool{false, true} {
			for _, source := range []string{payload, "# " + payload, "**" + payload + "**", "[label](" + payload + ")"} {
				out := strings.Join((readerBody{text: source, markdown: true}).render(64, plain, ""), "\n")
				safe := out
				if !plain {
					safe = sgr.ReplaceAllString(out, "")
				}
				if strings.ContainsFunc(safe, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
					t.Fatalf("plain=%v: terminal command survived Markdown rendering: %q", plain, out)
				}
			}
		}
	}
	// Reject 8-bit C1 controls and malformed UTF-8 as well as UTF-8 text controls.
	for _, input := range []string{"\x9b2J", "\x9d52;c;evil\x9c", "\x1b]52;c;unfinished", "\x1b[31mvalid\x1b[0m\xff"} {
		out := safeReaderOutput(input, false)
		safe := sgr.ReplaceAllString(out, "")
		if !utf8.ValidString(safe) || strings.ContainsFunc(safe, unicode.IsControl) {
			t.Fatalf("unsafe output: %q", out)
		}
	}
	styled := safeReaderOutput("\x1b[38;2;255;100;0mheading\x1b[0m", false)
	if !strings.Contains(styled, "\x1b[") {
		t.Fatal("filter removed safe styling")
	}
}

func TestExploreReaderWrapsLongMarkdownTokens(t *testing.T) {
	tokens := []string{strings.Repeat("identifier", 30), "https://example.com/" + strings.Repeat("segment", 40), strings.Repeat("界", 100)}
	for _, width := range []int{18, 24, 40, 64} {
		for _, plain := range []bool{false, true} {
			for _, token := range tokens {
				for _, source := range []string{token, "`" + token + "`", "```\n" + token + "\n```"} {
					lines := (readerBody{text: source, markdown: true}).render(width, plain, "")
					var text strings.Builder
					for _, line := range lines {
						if ansi.StringWidth(line) > width {
							t.Fatalf("width %d overflow: %q", width, line)
						}
						text.WriteString(strings.TrimSpace(ansi.Strip(line)))
					}
					if !strings.Contains(text.String(), token) {
						t.Fatalf("width %d plain=%v lost token: %q", width, plain, text.String())
					}
				}
			}
		}
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

func TestExploreListingSelectionSurvivesTheStatusRow(t *testing.T) {
	m := screenFeed(t, 80, 12, intexplore.ProtocolOff, 3)
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

// Rendering a long document takes long enough to notice, and dragging a
// window edge sends a size for every step. Only a new width needs new lines.
func TestExploreReaderKeepsItsLinesWhenOnlyTheHeightChanges(t *testing.T) {
	m := screenFeed(t, 80, 24, intexplore.ProtocolOff, 3)
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.reading || m.reader.wrapped != m.contentWidth() {
		t.Fatalf("fixture: reading=%v wrapped=%d", m.reading, m.reader.wrapped)
	}
	m.reader.body = readerBody{text: strings.Repeat("a line of the document that wraps when the reader narrows\n", 60)}
	m.reader.lines = m.reader.body.render(m.contentWidth(), true, "")
	// A sentinel no render would produce: still there means no render ran.
	m.reader.lines[0] = "rendered once"
	m.reader.offset = len(m.reader.lines)
	for _, height := range []int{30, 12, 40, 24} {
		m, _ = press(t, m, tea.WindowSizeMsg{Width: 80, Height: height})
		if m.reader.lines[0] != "rendered once" {
			t.Fatalf("a resize to 80x%d rendered the document again", height)
		}
		if want := maxReaderOffset(len(m.reader.lines), len(m.reader.lines), m.bodyHeight()); m.reader.offset > want {
			t.Fatalf("offset %d past the end %d at height %d", m.reader.offset, want, height)
		}
		if problems := checkLayout(m); len(problems) > 0 {
			t.Fatal(problems)
		}
	}
	m, _ = press(t, m, tea.WindowSizeMsg{Width: 50, Height: 24})
	if m.reader.lines[0] == "rendered once" || m.reader.wrapped != m.contentWidth() {
		t.Fatal("a new width did not render the document again")
	}
	for _, line := range m.reader.lines {
		if cellWidth(line) > m.contentWidth() {
			t.Fatalf("line wider than the narrowed reader: %q", line)
		}
	}
}

// Space toggles a replay the reader does not draw. It is not a reader key.
func TestExploreReaderIgnoresSpace(t *testing.T) {
	m := screenFeed(t, 100, 30, intexplore.ProtocolKitty, 6)
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

func TestExploreReaderGoKeepsOpenedItemAfterRefresh(t *testing.T) {
	for _, change := range []string{"insert", "remove", "empty"} {
		m := replayModel(intexplore.ProtocolOff)
		m.root = "/camp"
		m.gotoEnabled = true
		// A festival without a goal uses its indexed summary, requiring no filesystem fixture.
		original := m.visible.Items[0]
		next, _ := m.openReader()
		m = next.(exploreModel)
		if !m.reading {
			t.Fatal("reader did not open")
		}
		replacement := original
		replacement.Path = "newer/item"
		replacement.Title = "Replacement"
		replacement.DoneDate = "2099-01-01"
		items := []intexplore.Item{replacement, original}
		if change == "remove" {
			items = []intexplore.Item{replacement}
		}
		if change == "empty" {
			items = nil
		}
		m, _ = press(t, m, exploreLoaded{index: intexplore.Index{Items: items}, changed: true})
		m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
		if m.gotoPath != exploreJump(m.root, original) || !m.quitting {
			t.Fatalf("%s navigated to %q", change, m.gotoPath)
		}
	}
}
