package explore

import (
	"context"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"regexp"
	"strconv"
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

// screenFeed is a feed of n festivals, two a day. Every third has no replay
// and every fifth is archived, so moving focus and changing the status lens
// both pass through rows with and without an image.
func screenFeed(t *testing.T, w, h int, protocol string, n int) exploreModel {
	t.Helper()
	items := make([]intexplore.Item, n)
	for i := range items {
		day := time.Date(2026, time.March, 28-i/2, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		path := fmt.Sprintf("festivals/.dungeon/completed/%s/item-%02d", day, i)
		items[i] = intexplore.Item{
			DoneDate: day, DateSource: intexplore.DateBucket, Status: "completed",
			DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival,
			Title: fmt.Sprintf("Item %02d", i), ID: fmt.Sprintf("FA%04d", i),
			Summary: "A finished summary long enough to be cut short in a narrow pane",
			Path:    path, Replay: path + "/festival-replay.gif", IsDir: true,
		}
		if i%3 == 2 {
			items[i].Replay = ""
		}
		if i%5 == 4 {
			items[i].Status = "archived"
		}
	}
	m := exploreModel{
		ctx: context.Background(), width: w, height: h, protocol: protocol,
		reduced: true, // a still poster: nothing repaints the image but the view
		query:   intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll},
		index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
			Items:    items,
			BuiltAt:  time.Date(2026, time.March, 29, 0, 0, 0, 0, time.UTC),
		},
	}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	return m
}

// emojiTitles gives three rows in four a title with an emoji sequence: a ZWJ
// sequence, a skin tone modifier, a flag. A terminal that joins the sequence
// draws two cells and one that does not draws each code point, so these are
// the rows whose width a layout gets wrong.
func emojiTitles(t *testing.T, m exploreModel) exploreModel {
	t.Helper()
	sequences := []string{"", "👨\u200d💻", "👍🏽", "🇺🇸"}
	m.index.Items = append([]intexplore.Item(nil), m.index.Items...)
	for i := range m.index.Items {
		if seq := sequences[i%len(sequences)]; seq != "" {
			m.index.Items[i].Title += " " + seq
			m.index.Items[i].Summary = seq + " " + m.index.Items[i].Summary
		}
	}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	return m
}

// posterFor is a solid PNG whose color depends on the item, so two items
// never share image bytes.
func posterFor(path string, w, h int) []byte {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(path))
	v := sum.Sum32()
	img := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	fill := color.RGBA{R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16), A: 255}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = fill.R, fill.G, fill.B, fill.A
	}
	png, err := intexplore.EncodePNG(img)
	if err != nil {
		panic(err)
	}
	return png
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

var (
	// viewPanePlacement is how the wide layout puts the right pane at its
	// column: the explorer's own cursor control, never repository text.
	viewPanePlacement = regexp.MustCompile(`\x1b\[K\x1b\[\d+G`)

	viewImageAt   = regexp.MustCompile(`\x1b7\x1b\[(\d+);(\d+)H`)
	viewKittySize = regexp.MustCompile(`,c=(\d+),r=(\d+),`)
	viewITermSize = regexp.MustCompile(`width=(\d+);height=(\d+):`)
	viewKittyAPC  = regexp.MustCompile(`\x1b_G[^\x1b]*\x1b\\`)
	viewITermOSC  = regexp.MustCompile(`\x1b\][^\a]*\a`)
	viewCSI       = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)
	viewCSIErase  = regexp.MustCompile(`\x1b\[\d*X`)
)

// imageBox is where a view asks for its replay: one-based row and column of
// the top left cell and the size in cells. ok is false when it asks for none.
func imageBox(view string) (row, col, cols, rows int, ok bool) {
	at := viewImageAt.FindStringSubmatch(view)
	if at == nil {
		return 0, 0, 0, 0, false
	}
	row, _ = strconv.Atoi(at[1])
	col, _ = strconv.Atoi(at[2])
	size := viewKittySize.FindStringSubmatch(view)
	if size == nil {
		size = viewITermSize.FindStringSubmatch(view)
	}
	if size == nil {
		return 0, 0, 0, 0, false
	}
	cols, _ = strconv.Atoi(size[1])
	rows, _ = strconv.Atoi(size[2])
	return row, col, cols, rows, true
}

// viewText strips image payloads and control sequences from one view line.
func viewText(line string) string {
	line = viewKittyAPC.ReplaceAllString(line, "")
	line = viewITermOSC.ReplaceAllString(line, "")
	line = strings.NewReplacer("\x1b7", "", "\x1b8", "").Replace(line)
	return viewCSI.ReplaceAllString(line, "")
}

// checkLayout holds every view to the screen it is drawn on: exactly height
// rows, no row wider than the terminal on any terminal, the status and help
// rows last, the focused row and its card on screen, and the replay inside
// the body with no text beneath it.
func checkLayout(m exploreModel) []string {
	if m.height < 4 || m.width < 20 {
		return nil
	}
	var problems []string
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		return []string{fmt.Sprintf("%d rows, want %d", len(lines), m.height)}
	}
	for i, line := range lines {
		if w := cellWidth(viewText(line)); w > m.width {
			problems = append(problems, fmt.Sprintf("row %d is %d columns wide: %q", i+1, w, viewText(line)))
		}
	}
	help := m.helpLines()
	lastBody := m.height - len(help)
	for i, want := range help {
		if got := lines[lastBody+i]; viewText(got) != viewText(want) {
			problems = append(problems, fmt.Sprintf("row %d = %q, want the help row %q", lastBody+i+1, viewText(got), viewText(want)))
		}
	}
	if status := m.shownStatus(); status != "" {
		if got := viewText(lines[lastBody-1]); got != fit(status, m.width) {
			problems = append(problems, fmt.Sprintf("row %d = %q, want the status %q", lastBody, got, status))
		}
		lastBody--
	}
	firstBody := 2 + len(m.noticeLines())
	if firstBody > lastBody {
		return append(problems, fmt.Sprintf("no body row: first %d, last %d", firstBody, lastBody))
	}
	row, col, cols, rows, ok := imageBox(m.View())
	problems = append(problems, checkFocusBlock(m, lines[firstBody-1:lastBody], rows)...)
	if !ok {
		return problems
	}
	if row < firstBody || row+rows-1 > lastBody {
		problems = append(problems, fmt.Sprintf("replay rows %d-%d leave the body rows %d-%d", row, row+rows-1, firstBody, lastBody))
	}
	if col+cols-1 > m.width {
		problems = append(problems, fmt.Sprintf("replay columns %d-%d leave the %d-column terminal", col, col+cols-1, m.width))
	}
	for r := row; r < row+rows && r <= len(lines); r++ {
		text := strings.TrimRight(viewText(lines[r-1]), " ")
		if w := lipgloss.Width(text); w >= col {
			problems = append(problems, fmt.Sprintf("row %d has text under the replay at column %d: %q", r, col, text))
		}
	}
	return problems
}

// checkFocusBlock holds the feed to showing the focused row and, under it,
// the card: the only place the item's status, date, and path are drawn. The
// wide layout always has the room. The narrow one shows as many card lines
// as the body has rows under the focused row, its summary, and the replay.
func checkFocusBlock(m exploreModel, body []string, imageRows int) []string {
	item, ok := m.focused()
	if m.reading || !ok || len(m.index.Dungeons) == 0 {
		return nil
	}
	at := -1
	for i, line := range body {
		if strings.HasPrefix(viewText(line), "> ") {
			at = i
			break
		}
	}
	if at < 0 {
		return []string{"the focused row is not on screen"}
	}
	width, from, room := m.width, at+1, len(body)
	if m.wide() {
		width, from = m.width-m.width/2, 0
	} else {
		room = len(body) - at - 1
		if item.Summary != "" {
			room--
		}
		if imageRows > 0 {
			room -= imageRows + 1
		}
	}
	card := m.cardLines(item, width)
	next := from
	for i, want := range card[:min(max(room, 0), len(card))] {
		text := strings.TrimRight(viewText(want), " ")
		found := false
		for ; next < len(body) && !found; next++ {
			found = strings.HasSuffix(strings.TrimRight(viewText(body[next]), " "), text)
		}
		if !found {
			return []string{fmt.Sprintf("card line %d of %d (%q) is not on screen under the focused row", i+1, len(card), text)}
		}
	}
	return nil
}
