package dungeon

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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
				out := strings.Join((readerBody{text: source, markdown: true}).render(64, plain), "\n")
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
					lines := (readerBody{text: source, markdown: true}).render(width, plain)
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

func TestExploreReaderGoKeepsOpenedItemAfterRefresh(t *testing.T) {
	for _, change := range []string{"insert", "remove", "empty"} {
		m := replayModel(explore.ProtocolOff)
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
		items := []explore.Item{replacement, original}
		if change == "remove" {
			items = []explore.Item{replacement}
		}
		if change == "empty" {
			items = nil
		}
		m, _ = press(t, m, exploreLoaded{index: explore.Index{Items: items}, changed: true})
		m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
		if m.gotoPath != exploreJump(m.root, original) || !m.quitting {
			t.Fatalf("%s navigated to %q", change, m.gotoPath)
		}
	}
}
