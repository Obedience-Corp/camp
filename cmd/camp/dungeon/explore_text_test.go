package dungeon

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

// Emoji sequences are the text terminals disagree about: two cells where the
// terminal joins them, every code point's cells where it does not.
var emojiSequences = map[string]string{
	"family":    "👩\u200d👩\u200d👧\u200d👦",
	"developer": "👨\u200d💻",
	"skin tone": "👍🏽",
	"flag":      "🇺🇸",
}

func TestExploreCellWidthIsTheWidestATerminalDraws(t *testing.T) {
	for name, seq := range emojiSequences {
		apart := 0
		for _, r := range seq {
			apart += lipgloss.Width(string(r))
		}
		if got := cellWidth(seq); got < apart || got < lipgloss.Width(seq) {
			t.Errorf("%s: cellWidth = %d, below the %d cells of its code points or the %d of the cluster", name, got, apart, lipgloss.Width(seq))
		}
	}
	// Text every terminal agrees on is measured as before.
	for _, plain := range []string{"", "abc", "界面", "e\u0301", "a·b…", "\x1b[1mbold\x1b[0m"} {
		if got, want := cellWidth(plain), lipgloss.Width(plain); got != want {
			t.Errorf("cellWidth(%q) = %d, want %d", plain, got, want)
		}
	}
	for name, seq := range emojiSequences {
		long := strings.Repeat(seq+" word ", 20)
		for _, width := range []int{1, 2, 7, 20, 41} {
			if got := fit(long, width); cellWidth(got) > width {
				t.Errorf("%s: fit to %d is %d cells: %q", name, width, cellWidth(got), got)
			}
			if got := padWidth(fit(long, width), width); cellWidth(got) != width && cellWidth(got) != width-1 {
				t.Errorf("%s: padded to %d is %d cells", name, width, cellWidth(got))
			}
			if got := keepTail(long, "  Festivals", width); cellWidth(got) > width {
				t.Errorf("%s: keepTail at %d is %d cells: %q", name, width, cellWidth(got), got)
			}
			if got := keepEnd("/ ", long, width); cellWidth(got) > width {
				t.Errorf("%s: keepEnd at %d is %d cells: %q", name, width, cellWidth(got), got)
			}
		}
	}
}

// The focused row is padded to the full width for its highlight. Measured by
// cluster, a title with an emoji sequence came out wider than the terminal
// wherever the terminal draws the sequence apart: the row wrapped, the screen
// scrolled, and the header was gone.
func TestExploreEmojiSequencesStayInsideTheScreen(t *testing.T) {
	for name, seq := range emojiSequences {
		for _, size := range [][2]int{{80, 24}, {80, 12}, {120, 40}, {40, 10}} {
			label := fmt.Sprintf("%s at %dx%d", name, size[0], size[1])
			m := screenFeed(t, size[0], size[1], explore.ProtocolOff, 8)
			for i := range m.visible.Items {
				item := &m.visible.Items[i]
				item.Title = "Ship " + seq + " " + item.Title
				item.Summary = seq + " " + item.Summary + " " + seq
				item.DungeonLabel = "Fest " + seq
			}
			s := openExploreScreen(t, m)
			for _, key := range []string{"j", "j", "k", "j", "?", "?", "/", "esc"} {
				got := s.send(screenKey(key))
				if s.grid.wrapped > 0 || s.grid.scrolled > 0 {
					t.Fatalf("%s after %s: %d rows wrapped, %d scrolls\n%s", label, key, s.grid.wrapped, s.grid.scrolled, s.grid)
				}
				if problems := checkLayout(got); len(problems) > 0 {
					t.Fatalf("%s after %s: %v", label, key, problems)
				}
			}
			if got := s.grid.rowText(0); !strings.HasPrefix(got, "All dungeons") {
				t.Fatalf("%s: header row = %q\n%s", label, got, s.grid)
			}
			if got := s.grid.rowText(size[1] - 1); !strings.Contains(got, "q quit") {
				t.Fatalf("%s: help row = %q\n%s", label, got, s.grid)
			}

			// The reader pads its rows too: a listing's selection, and every
			// row Glamour lays out.
			reading := m
			reading.reading = true
			reading.reader = exploreReader{title: "docs " + seq, listing: true, item: m.visible.Items[0]}
			for i := range 6 {
				reading.reader.entries = append(reading.reader.entries, readerEntry{name: strings.Repeat(seq+"-notes-", 12) + fmt.Sprint(i) + ".md"})
			}
			doc := reading
			body := readerBody{markdown: true, text: "# Notes " + seq + "\n\n" + strings.Repeat("word "+seq+" ", 60) + "\n\n- item " + seq + "\n\n`code " + strings.Repeat(seq, 30) + "`\n"}
			doc.reader = exploreReader{title: "notes.md", item: m.visible.Items[0], body: body}
			doc.reader.lines = body.render(doc.contentWidth(), true)
			for mode, view := range map[string]exploreModel{"listing": reading, "document": doc} {
				s := openExploreScreen(t, view)
				s.send(screenKey("j"))
				if s.grid.wrapped > 0 || s.grid.scrolled > 0 {
					t.Fatalf("%s reader %s: %d rows wrapped, %d scrolls\n%s", label, mode, s.grid.wrapped, s.grid.scrolled, s.grid)
				}
				if got := s.grid.rowText(size[1] - 1); !strings.Contains(got, "esc back") {
					t.Fatalf("%s reader %s: help row = %q\n%s", label, mode, got, s.grid)
				}
			}
		}
	}
}

// Padding that Glamour adds by its own measure must not become a row of its
// own when the reader's stricter measure breaks the line before it.
func TestExploreReaderWrapDropsPaddingOnlyRows(t *testing.T) {
	seq := emojiSequences["family"]
	padded := "ab " + seq + strings.Repeat(" ", 10)
	rows := wrapPlain(padded+"\nnext", cellWidth("ab "+seq)+2)
	if len(rows) != 2 || rows[1] != "next" {
		t.Fatalf("rows = %q, want the text row and the next line", rows)
	}
	if rows := wrapPlain("a\n\n   \nb", 10); len(rows) != 4 {
		t.Fatalf("blank source lines must stay: %q", rows)
	}
	if rows := wrapPlain(strings.Repeat("x", 25), 10); len(rows) != 3 || rows[2] != "xxxxx" {
		t.Fatalf("hard wrap = %q", rows)
	}
	styled := "\x1b[31m" + strings.Repeat("y", 12) + "\x1b[0m"
	if rows := wrapPlain(styled, 10); len(rows) != 2 || !strings.HasPrefix(rows[0], "\x1b[31m") || rows[1] != "yy\x1b[0m" {
		t.Fatalf("styled hard wrap = %q", rows)
	}
}

// Glamour's dark style draws body text in light grey. On a light terminal the
// feed switches palette and the reader has to follow it.
func TestExploreReaderFollowsTheTerminalBackground(t *testing.T) {
	oldProfile, wasDark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer func() {
		lipgloss.SetColorProfile(oldProfile)
		lipgloss.SetHasDarkBackground(wasDark)
	}()
	body := regexp.MustCompile(`\x1b\[([0-9;]*)mbody`)
	colors := map[bool]string{}
	for _, dark := range []bool{true, false} {
		lipgloss.SetHasDarkBackground(dark)
		found := body.FindStringSubmatch(strings.Join(renderExploreMarkdown("body text\n", 40, false), "\n"))
		if found == nil {
			t.Fatalf("dark=%v: body text carries no color", dark)
		}
		colors[dark] = found[1]
	}
	// 252 is light grey, 234 is near black: Glamour's dark and light body text.
	if colors[true] != "38;5;252" || colors[false] != "38;5;234" {
		t.Fatalf("body color on a dark terminal %q, on a light one %q; want 38;5;252 and 38;5;234", colors[true], colors[false])
	}
}

// The source is cleaned before Glamour reads it, but Glamour decodes
// entities, so a bidi override spelled &#x202E; reached the terminal.
func TestExploreReaderCleansWhatEntitiesDecodeTo(t *testing.T) {
	doc := "abc&#x202E;def &#8238;ghi zero&#x200B;width soft&#xAD;hyphen bom&#xFEFF;end\n\n" +
		"join&#x200D;er stays, &amp; and &lt; decode, esc &#27;[31mred&#27;[0m osc &#27;]0;title&#7; done\n"
	for _, plain := range []bool{true, false} {
		out := strings.Join(readerBody{text: doc, markdown: true}.render(72, plain), "\n")
		for _, r := range out {
			if r != '\n' && r != 0x1b && explore.CleanText(string(r)) == "" {
				t.Errorf("plain=%v: U+%04X reached the reader: %q", plain, r, out)
			}
		}
		text := viewText(out)
		for _, want := range []string{"abcdef", "ghi", "zerowidth", "softhyphen", "bomend", "join\u200der", "& and <", "done"} {
			if !strings.Contains(text, want) {
				t.Errorf("plain=%v: %q is missing from %q", plain, want, text)
			}
		}
		if strings.Contains(out, "\x1b]") || strings.Contains(out, "\a") {
			t.Errorf("plain=%v: an entity-built OSC reached the reader: %q", plain, out)
		}
	}
}

func TestExploreCutNoticeFitsANarrowReader(t *testing.T) {
	for _, width := range []int{18, 30, 38, 78} {
		lines := readerBody{text: "x", lead: cutNotice(true)}.render(width, true)
		for _, line := range lines {
			if w := cellWidth(line); w > width {
				t.Fatalf("at %d columns a notice row is %d wide: %q", width, w, line)
			}
		}
		if joined := strings.Join(strings.Fields(strings.Join(lines, " ")), ""); !strings.Contains(joined, "Showingthefirst256KiBofthisfile.") {
			t.Fatalf("at %d columns the notice lost text: %q", width, lines)
		}
	}
}
