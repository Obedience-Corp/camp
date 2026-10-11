package dungeon

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// Emoji sequences are the text terminals disagree about: two cells where the
// terminal joins them, every code point's cells where it does not.
var emojiSequences = map[string]string{
	"family":    "👩\u200d👩\u200d👧\u200d👦",
	"developer": "👨\u200d💻",
	"skin tone": "👍🏽",
	"flag":      "🇺🇸",
	// A digit, U+FE0F, and U+20E3: the one sequence that is wider joined
	// (two cells) than as its code points (one).
	"keycap": "1\ufe0f\u20e3",
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
			if got := padWidth(fit(long, width), width); cellWidth(got) != width {
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
	for _, kind := range screenKinds {
		for name, seq := range emojiSequences {
			for _, size := range [][2]int{{80, 24}, {80, 12}, {120, 40}, {40, 10}} {
				emojiSequenceStaysInside(t, kind.open, fmt.Sprintf("%s, %s at %dx%d", kind.name, name, size[0], size[1]), seq, size)
			}
		}
	}
}

func emojiSequenceStaysInside(t *testing.T, open func(*testing.T, exploreModel) *exploreScreen, label, seq string, size [2]int) {
	t.Helper()
	m := screenFeed(t, size[0], size[1], explore.ProtocolOff, 8)
	for i := range m.visible.Items {
		item := &m.visible.Items[i]
		item.Title = "Ship " + seq + " " + item.Title
		item.Summary = seq + " " + item.Summary + " " + seq
		item.DungeonLabel = "Fest " + seq
	}
	s := open(t, m)
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
	doc.reader.lines = body.render(doc.contentWidth(), true, "")
	for mode, view := range map[string]exploreModel{"listing": reading, "document": doc} {
		s := open(t, view)
		s.send(screenKey("j"))
		if s.grid.wrapped > 0 || s.grid.scrolled > 0 {
			t.Fatalf("%s reader %s: %d rows wrapped, %d scrolls\n%s", label, mode, s.grid.wrapped, s.grid.scrolled, s.grid)
		}
		if got := s.grid.rowText(size[1] - 1); !strings.Contains(got, "esc back") {
			t.Fatalf("%s reader %s: help row = %q\n%s", label, mode, got, s.grid)
		}
	}
}

// Glamour lays a table out by a measure that counts a keycap as one cell, and
// the reader's wrap took an ASCII digit without the marks that make it a
// keycap. A row of eight came out 82 cells wide on an 80-column terminal that
// joins the sequence, and wrapped.
func TestExploreReaderMeasuresKeycapsAsDrawn(t *testing.T) {
	keycap := emojiSequences["keycap"]
	docs := map[string]string{
		"table":     "|" + strings.Repeat(" h |", 12) + "\n|" + strings.Repeat("---|", 12) + "\n|" + strings.Repeat(" "+keycap+" |", 12) + "\n",
		"long run":  "`" + strings.Repeat(keycap, 90) + "`\n",
		"paragraph": strings.Repeat("step "+keycap+" then ", 40) + "\n",
	}
	for name, doc := range docs {
		for _, width := range []int{38, 58, 78, 98} {
			for _, plain := range []bool{true, false} {
				for _, line := range (readerBody{text: doc, markdown: true}).render(width, plain, "") {
					if got := cellWidth(line); got > width {
						t.Fatalf("%s at %d (plain=%v): a row is %d cells: %q", name, width, plain, got, ansi.Strip(line))
					}
				}
			}
		}
	}
	if rows := wrapPlain(strings.Repeat(keycap, 6), 4); len(rows) != 3 {
		t.Fatalf("six keycaps at four cells = %q, want two to a row", rows)
	}
}

// padWidth measured with cellWidth and cut with ansi.Truncate, which counts
// some sequences wider. Text that already filled its width exactly was cut
// again: the row lost its ellipsis and the highlight ended short.
func TestExplorePadWidthKeepsTextThatAlreadyFits(t *testing.T) {
	// An ASCII base, a combining mark, and U+FE0F: one cell by cellWidth,
	// three by ansi.
	odd := "e\u0301\ufe0f"
	if cellWidth(odd) >= ansi.StringWidth(odd) {
		t.Fatalf("%q no longer measures wider by ansi (%d) than by cellWidth (%d); pick another", odd, ansi.StringWidth(odd), cellWidth(odd))
	}
	for _, text := range []string{"ab" + odd + "cd…", odd + odd + "…", "plain…"} {
		width := cellWidth(text)
		if got := padWidth(text, width); got != text {
			t.Errorf("padWidth(%q, %d) = %q, want it untouched", text, width, got)
		}
		if got := padWidth(text, width+3); got != text+"   " {
			t.Errorf("padWidth(%q, %d) = %q, want three spaces added", text, width+3, got)
		}
	}
	// Text wider than the width is cut to it, styling kept whole.
	styled := "\x1b[1m" + strings.Repeat("界", 6) + "\x1b[0m"
	for width := range 14 {
		got := padWidth(styled, width)
		if cellWidth(got) != width {
			t.Errorf("padWidth to %d is %d cells: %q", width, cellWidth(got), got)
		}
		if width > 0 && !strings.HasPrefix(got, "\x1b[1m") {
			t.Errorf("padWidth to %d lost the styling: %q", width, got)
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
		found := body.FindStringSubmatch(strings.Join(renderExploreMarkdown("body text\n", 40, false, ""), "\n"))
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

// The background is known only where the terminal exports COLORFGBG; Kitty,
// Ghostty, and WezTerm do not, and a light one of them is taken for dark. The
// theme setting is how a user corrects that, so the reader and the feed take
// the configured theme and not only the guess.
func TestExploreStylesFollowTheConfiguredTheme(t *testing.T) {
	oldProfile, wasDark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer func() {
		lipgloss.SetColorProfile(oldProfile)
		lipgloss.SetHasDarkBackground(wasDark)
	}()
	body := regexp.MustCompile(`\x1b\[([0-9;]*)mbody`)
	color := func(name theme.ThemeName) string {
		t.Helper()
		look := newExploreStyles(false, name)
		found := body.FindStringSubmatch(strings.Join(renderExploreMarkdown("body text\n", 40, false, look.glamour), "\n"))
		if found == nil {
			t.Fatalf("theme %q: body text carries no color", name)
		}
		return found[1]
	}
	// 252 is light grey, 234 is near black: Glamour's dark and light body text.
	for _, dark := range []bool{true, false} {
		lipgloss.SetHasDarkBackground(dark)
		for name, want := range map[theme.ThemeName]string{
			theme.ThemeLight: "38;5;234", theme.ThemeDark: "38;5;252", theme.ThemeHighContrast: "38;5;252",
		} {
			if got := color(name); got != want {
				t.Errorf("theme %q on a terminal taken for dark=%v: body color %q, want %q", name, dark, got, want)
			}
		}
	}
	// With no theme chosen the guess still decides.
	lipgloss.SetHasDarkBackground(true)
	if got := color(theme.ThemeAdaptive); got != "38;5;252" {
		t.Errorf("adaptive on a dark terminal: body color %q", got)
	}
	lipgloss.SetHasDarkBackground(false)
	if got := color(theme.ThemeAdaptive); got != "38;5;234" {
		t.Errorf("adaptive on a light terminal: body color %q", got)
	}
	// Plain output has no style to choose.
	if look := newExploreStyles(true, theme.ThemeLight); look.glamour != "" {
		t.Errorf("plain styles name a Glamour style: %q", look.glamour)
	}
}

// The source is cleaned before Glamour reads it, but Glamour decodes
// entities, so a bidi override spelled &#x202E; reached the terminal.
func TestExploreReaderCleansWhatEntitiesDecodeTo(t *testing.T) {
	doc := "abc&#x202E;def &#8238;ghi zero&#x200B;width soft&#xAD;hyphen bom&#xFEFF;end\n\n" +
		"join&#x200D;er stays, &amp; and &lt; decode, esc &#27;[31mred&#27;[0m osc &#27;]0;title&#7; done\n"
	for _, plain := range []bool{true, false} {
		out := strings.Join(readerBody{text: doc, markdown: true}.render(72, plain, ""), "\n")
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
	notice := cutNotice(true)[0]
	for _, width := range []int{18, 30, 38, 39, 78} {
		lines := readerBody{text: "x", lead: cutNotice(true)}.render(width, true, "")
		got := lines[0]
		if w := cellWidth(got); w > width {
			t.Fatalf("at %d columns the notice row is %d wide: %q", width, w, got)
		}
		switch {
		case width >= cellWidth(notice) && got != notice:
			t.Fatalf("at %d columns the notice is %q", width, got)
		case width < cellWidth(notice) && (!strings.HasSuffix(got, "…") || !strings.HasPrefix(notice, strings.TrimSuffix(got, "…"))):
			t.Fatalf("at %d columns the notice is cut without a mark: %q", width, got)
		}
	}
}
