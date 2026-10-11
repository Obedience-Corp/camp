package explore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

func TestExploreFeedShowsDayTitleAndDungeon(t *testing.T) {
	m := exploreModel{
		width:  100,
		height: 30,
		query:  intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll},
		index: intexplore.Index{Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon"}}, Items: []intexplore.Item{
			{DoneDate: "2026-10-05", DateSource: intexplore.DateBucket, Status: "completed", DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival, Title: "festival-activity-desktop", ID: "FA0024", Summary: "Ship Festival Activity", Path: "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024", IsDir: true},
			{DoneDate: "2026-08-21", DateSource: intexplore.DateHistory, Status: "done", DungeonLabel: "Intents", DungeonPath: ".campaign/intents/.dungeon", Kind: intexplore.KindMarkdown, Title: "Add blog sections", Path: ".campaign/intents/.dungeon/done/add-blog.md"},
		}},
		protocol: intexplore.ProtocolOff,
	}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	view := m.View()
	for _, want := range []string{"Festivals", "Finished", "festival-activity-desktop", "FA0024", "Add blog sections", "Intents"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	moved := next.(exploreModel)
	if moved.cursor != 1 {
		t.Fatalf("cursor = %d", moved.cursor)
	}
	opened, _ := moved.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	hopped := opened.(exploreModel)
	if hopped.quitting {
		t.Fatal("g quit without shell integration")
	}
	if !strings.Contains(hopped.status, "shell-init") {
		t.Fatalf("status = %q", hopped.status)
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

func TestExploreFilterPromptKeepsTheTypedEnd(t *testing.T) {
	m := screenFeed(t, 40, 12, intexplore.ProtocolOff, 6)
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
	short := screenFeed(t, 40, 12, intexplore.ProtocolOff, 6)
	short, _ = press(t, short, runes("/"))
	short, _ = press(t, short, runes("FA00"))
	if header := strings.Split(short.View(), "\n")[0]; header != "/ FA00" {
		t.Fatalf("short filter prompt = %q", header)
	}
}

// A lens or a filter changes the rows. The cursor stays on the item, as it
// does across a refresh; its old position is some other row.
func TestExploreFilterAndLensFollowTheFocusedItem(t *testing.T) {
	m := screenFeed(t, 100, 30, intexplore.ProtocolOff, 40)
	m.cursor = 7
	before, _ := m.focused()
	steps := map[string]func(exploreModel) exploreModel{
		"filter that keeps the item": func(m exploreModel) exploreModel {
			m, _ = press(t, m, runes("/"))
			m, _ = press(t, m, runes("Item"))
			m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			return m
		},
		"filter that narrows to the item's decade": func(m exploreModel) exploreModel {
			m, _ = press(t, m, runes("/"))
			m, _ = press(t, m, runes(before.Title[:len(before.Title)-1]))
			m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			return m
		},
		"status lens": func(m exploreModel) exploreModel { m, _ = press(t, m, runes("s")); return m },
		"dungeon lens": func(m exploreModel) exploreModel {
			m, _ = press(t, m, runes("]"))
			return m
		},
	}
	for name, step := range steps {
		got := step(m)
		after, ok := got.focused()
		held := false
		for _, item := range got.visible.Items {
			held = held || item.Path == before.Path
		}
		switch {
		case !ok:
			t.Errorf("%s: no focused row among %d", name, len(got.visible.Items))
		case held && after.Path != before.Path:
			t.Errorf("%s: focus moved from %q to %q though the item is still listed", name, before.Title, after.Title)
		case !held && got.cursor != 0:
			t.Errorf("%s: the item is gone and the cursor is on row %d, not the first", name, got.cursor)
		}
		if problems := checkLayout(got); len(problems) > 0 {
			t.Errorf("%s: %v", name, problems)
		}
	}
	// The item drops out of the rows: start at the top, not at row 7.
	gone := m
	gone, _ = press(t, gone, runes("/"))
	gone, _ = press(t, gone, runes("Item 1"))
	gone, _ = press(t, gone, tea.KeyMsg{Type: tea.KeyEnter})
	if len(gone.visible.Items) < 8 || gone.cursor != 0 {
		t.Fatalf("filter without the focused item: cursor %d of %d", gone.cursor, len(gone.visible.Items))
	}
}

// While the filter is typed q, enter, and the rest of the feed keys do other
// things. The help row says what does act, and ctrl+c quits as everywhere.
func TestExploreFilterModeNamesItsKeys(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := screenFeed(t, width, 24, intexplore.ProtocolOff, 6)
		m, _ = press(t, m, runes("/"))
		help := viewText(strings.Join(m.helpLines(), " "))
		for _, want := range []string{"enter apply", "esc cancel", "ctrl+c quit"} {
			if !strings.Contains(help, want) {
				t.Errorf("%d columns: filter help %q does not name %q", width, help, want)
			}
		}
		for _, wrong := range []string{"q quit", "enter read", "j/k"} {
			if strings.Contains(help, wrong) {
				t.Errorf("%d columns: filter help %q names %q, which types or applies here", width, help, wrong)
			}
		}
		if problems := checkLayout(m); len(problems) > 0 {
			t.Errorf("%d columns: %v", width, problems)
		}
		typed, _ := press(t, m, runes("q"))
		if typed.quitting || typed.filter != "q" {
			t.Fatalf("q in the filter: quitting=%v filter=%q", typed.quitting, typed.filter)
		}
		// An applied filter is named in the header even where the header is cut.
		applied, _ := press(t, typed, tea.KeyMsg{Type: tea.KeyEnter})
		if header := viewText(strings.Split(applied.View(), "\n")[0]); applied.query.Text != "q" || !strings.Contains(header, `"q"`) {
			t.Errorf("%d columns: the header %q does not show the filter in force", width, header)
		}
		back, _ := press(t, typed, tea.KeyMsg{Type: tea.KeyEsc})
		if back.filtering || back.quitting || back.query.Text != "" {
			t.Fatalf("esc did not cancel the filter: %+v", back.query)
		}
		if help := viewText(strings.Join(back.helpLines(), " ")); !strings.Contains(help, "q quit") {
			t.Fatalf("the feed help did not return: %q", help)
		}
	}
	// ctrl+c quits from the feed, the full help, the filter, a listing, and a document.
	feed := screenFeed(t, 80, 24, intexplore.ProtocolOff, 6)
	filter, _ := press(t, feed, runes("/"))
	help, _ := press(t, feed, runes("?"))
	doc, _ := press(t, feed, tea.KeyMsg{Type: tea.KeyEnter})
	listing := doc
	listing.reader.listing = true
	for name, m := range map[string]exploreModel{"feed": feed, "help": help, "filter": filter, "document": doc, "listing": listing} {
		quit, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
		if !quit.quitting || cmd == nil {
			t.Errorf("ctrl+c in the %s did not quit", name)
		}
	}
}

// A header too narrow for all of its parts cuts the lens, not the filter:
// the filter is the only thing on screen that says why rows are missing.
func TestExploreCutHeaderStillNamesTheFilter(t *testing.T) {
	for _, width := range []int{20, 24, 30, 40, 60, 100} {
		for _, filter := range []string{"q", "fest", "a filter much longer than the header has room for"} {
			m := screenFeed(t, width, 12, intexplore.ProtocolOff, 6)
			m.visible.DungeonLabel = "A dungeon with a long label"
			m.query.Text = filter
			header := viewText(strings.Split(m.View(), "\n")[0])
			if w := cellWidth(header); w > width {
				t.Fatalf("%d columns, filter %q: header is %d wide: %q", width, filter, w, header)
			}
			// The quoted filter in full, or its start and the mark of a cut.
			at := strings.Index(header, " · \""+filter[:1])
			if at < 0 {
				t.Errorf("%d columns: the header %q does not name the filter %q", width, header, filter)
				continue
			}
			if shown := header[at+len(" · "):]; !strings.HasPrefix(shown, `"`+filter+`"`) && !strings.HasSuffix(shown, "…") {
				t.Errorf("%d columns: the header %q cuts the filter %q without a mark", width, header, filter)
			}
			if problems := checkLayout(m); len(problems) > 0 {
				t.Errorf("%d columns, filter %q: %v", width, filter, problems)
			}
		}
	}
	// Room for everything: the header reads in full, sort order last.
	m := screenFeed(t, 100, 12, intexplore.ProtocolOff, 6)
	m.query.Text = "fest"
	if header := viewText(strings.Split(m.View(), "\n")[0]); header != `All dungeons · Finished · "fest" · newest first` {
		t.Fatalf("full header = %q", header)
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
		m := screenFeed(t, width, 24, intexplore.ProtocolOff, 6)
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
		document := m.helpLines()
		m.reader.listing = true
		listing := m.helpLines()
		for _, key := range readerKeys {
			if !has(listing, key) {
				t.Errorf("%d columns: listing help does not name %q: %q", width, key, listing)
			}
			// enter opens a listed document; a document has nothing to open.
			if has(document, key) == (key == "enter") {
				t.Errorf("%d columns: document help and %q: %q", width, key, document)
			}
		}
	}

	// The command help lists the same keys, feed and reader each under its
	// own heading, spelled as the help rows spell them.
	sections := longKeySections(Cmd.Long)
	for heading, table := range map[string][]helpHint{"Keys in the feed:": exploreFullHelp, "Keys in the reader:": exploreReaderHelp} {
		for _, hint := range table {
			if key := hintKey(hint); !sections[heading][key] {
				t.Errorf("camp dungeon explore --help: %q does not list the key %q", heading, key)
			}
		}
	}

	// ? opens the list and ? or esc closes it; a short terminal keeps a body row.
	m := screenFeed(t, 80, 4, intexplore.ProtocolOff, 6)
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

// --images kitty forces a protocol where the automatic choice is off, but not
// in plain output. The help said "forces" with no exception.
func TestExploreHelpSaysPlainOutputDrawsNoReplay(t *testing.T) {
	long := strings.Join(strings.Fields(Cmd.Long), " ")
	for _, want := range []string{"NO_COLOR", "--no-color", "no replay is drawn, whatever --images says"} {
		if !strings.Contains(long, want) {
			t.Errorf("camp dungeon explore --help does not say %q", want)
		}
	}
	usage := Cmd.Flags().Lookup("images").Usage
	if !strings.Contains(usage, "NO_COLOR") || !strings.Contains(usage, "--no-color") {
		t.Errorf("--images help does not name plain output: %q", usage)
	}
}

// tmux, screen, and zellij do not pass an image on, and tmux takes a Kitty
// sequence for its pane title. The help said --images forces a protocol there.
func TestExploreHelpSaysAMultiplexerDrawsNoReplay(t *testing.T) {
	long := strings.Join(strings.Fields(Cmd.Long), " ")
	if want := "Inside tmux, screen, or zellij no replay is drawn, whatever --images says"; !strings.Contains(long, want) {
		t.Errorf("camp dungeon explore --help does not say %q", want)
	}
	forces := strings.Index(long, "forces a protocol there")
	if forces < 0 {
		t.Fatal("the help no longer says where --images forces a protocol")
	}
	sentence := long[strings.LastIndex(long[:forces], ". ")+1 : forces]
	for _, name := range []string{"tmux", "screen", "zellij"} {
		if strings.Contains(sentence, name) {
			t.Errorf("the help still says --images forces a protocol inside %s: %q", name, sentence)
		}
	}
	if usage := Cmd.Flags().Lookup("images").Usage; !strings.Contains(usage, "tmux, screen, or zellij") {
		t.Errorf("--images help does not name the multiplexers: %q", usage)
	}
}

// imagesCommand is a command with the explorer's replay flags, parsed.
func imagesCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "explore"}
	cmd.Flags().String("images", intexplore.ProtocolAuto, "")
	cmd.Flags().Bool("no-media", false, "")
	cmd.SetContext(context.Background())
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return cmd
}

// A protocol named inside a multiplexer is not sent. The feed says so once,
// on the row that answers a key: the request was explicit and nothing else
// on screen explains the missing replay.
func TestExploreSaysOnceThatAMultiplexerDrawsNoReplay(t *testing.T) {
	// The developer's own terminal must not decide the result, and nothing
	// is read from their configuration.
	for _, name := range []string{"TMUX", "STY", "ZELLIJ", "NO_COLOR"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent-camp-explore-test/config")
	feed := screenFeed(t, 100, 30, intexplore.ProtocolOff, 6)
	open := func(args ...string) exploreModel {
		t.Helper()
		cmd := imagesCommand(t, args...)
		flag, _ := cmd.Flags().GetString("images")
		protocol, err := intexplore.ChooseProtocol(flag, os.Getenv, false)
		if err != nil {
			t.Fatal(err)
		}
		m := newExploreModel(cmd, "/camp", "", feed.query, protocol, "", false, &feed.index)
		m.width, m.height = 100, 30
		return m
	}

	if m := open("--images", "kitty"); m.protocol != intexplore.ProtocolKitty || m.shownStatus() != "" {
		t.Fatalf("outside a multiplexer: protocol %q, status %q", m.protocol, m.shownStatus())
	}
	for name, env := range map[string][2]string{
		"tmux": {"TMUX", "/tmp/tmux-501/default,1,0"}, "screen": {"STY", "1234.pts-0.host"},
		"zellij": {"ZELLIJ", "0"}, "tmux across ssh": {"TERM", "tmux-256color"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(env[0], env[1])
			for _, forced := range []string{"kitty", "iterm", "KITTY"} {
				m := open("--images", forced)
				if m.protocol != intexplore.ProtocolOff {
					t.Fatalf("--images %s: protocol %q, want off", forced, m.protocol)
				}
				if m.shownStatus() != exploreMultiplexerNote || m.statusRow().failed || !strings.Contains(viewText(m.View()), exploreMultiplexerNote) {
					t.Fatalf("--images %s: the feed opens on status %q", forced, m.shownStatus())
				}
				if problems := checkLayout(m); len(problems) > 0 {
					t.Fatal(problems)
				}
				// Once: it goes with the first key, like any other answer.
				m, _ = press(t, m, runes("j"))
				if m.shownStatus() != "" {
					t.Fatalf("--images %s: after j the status is still %q", forced, m.shownStatus())
				}
			}
			// Nothing was asked for, or replays are off by another rule.
			for _, args := range [][]string{nil, {"--images", "auto"}, {"--images", "off"}, {"--images", "kitty", "--no-media"}} {
				if m := open(args...); m.shownStatus() != "" {
					t.Errorf("%v: the feed opens on status %q", args, m.shownStatus())
				}
			}
			t.Setenv("NO_COLOR", "1")
			if note := exploreImagesNote(imagesCommand(t, "--images", "kitty")); note != "" {
				t.Errorf("plain output: note %q", note)
			}
		})
	}
}
