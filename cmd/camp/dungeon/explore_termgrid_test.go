package dungeon

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

// termGrid is the part of a terminal the explorer depends on: a grid of
// cells, a cursor, autowrap, scrolling, tab stops, the erase commands Bubble
// Tea's renderer sends, and the two image protocols. An iTerm image lives in
// the cells it covers and goes when they are written or erased; it keeps its
// aspect, so it covers only the part of the requested box the picture fills.
// A Kitty image is a separate layer that only a delete command removes.
//
// Every code point takes its own cells, as on a terminal that does not join
// emoji sequences: the widest a row can come out.
//
// Tests assert on what the grid holds after the real renderer has written
// to it, which is what a person would see, not on the strings View returns.
type termGrid struct {
	w, h       int
	cells      [][]termCell
	row, col   int
	srow, scol int
	sgr        string
	kitty      map[int]termImage
	pending    []byte

	wrapped      int // rows that ran past the right margin
	scrolled     int // line feeds on the last row
	tabs         int // raw tabs received
	styledErases int // erases sent while a style was still open
}

type termCell struct {
	ch    rune
	image string // iTerm: the paint that owns this cell
}

type termImage struct {
	tag                  string
	row, col, cols, rows int
}

func newTermGrid(w, h int) *termGrid {
	g := &termGrid{w: w, h: h, kitty: map[int]termImage{}}
	g.cells = make([][]termCell, h)
	for i := range g.cells {
		g.cells[i] = make([]termCell, w)
	}
	return g
}

func termTag(payload string, row, col int) string {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(payload))
	return fmt.Sprintf("%x@%d,%d", sum.Sum32(), row, col)
}

func (g *termGrid) erase(row, from, to int) {
	if g.sgr != "" {
		g.styledErases++
	}
	if row < 0 || row >= g.h {
		return
	}
	for c := max(from, 0); c < min(to, g.w); c++ {
		g.cells[row][c] = termCell{}
	}
}

func (g *termGrid) lineFeed() {
	if g.row < g.h-1 {
		g.row++
		return
	}
	g.scrolled++
	g.cells = append(g.cells[1:], make([]termCell, g.w))
}

// Write feeds terminal output to the grid. A sequence split across writes is
// kept until the rest arrives.
func (g *termGrid) Write(p []byte) (int, error) {
	g.feed(append(g.pending, p...))
	return len(p), nil
}

func (g *termGrid) feed(data []byte) {
	g.pending = nil
	for i := 0; i < len(data); {
		switch b := data[i]; {
		case b == 0x1b:
			n, complete := g.escape(data[i:])
			if !complete {
				g.pending = append([]byte(nil), data[i:]...)
				return
			}
			i += n
		case b == '\r':
			g.col = 0
			i++
		case b == '\n':
			g.lineFeed()
			i++
		case b == '\t':
			g.tabs++
			g.col = min(g.col+8-g.col%8, g.w-1)
			i++
		case b < 0x20:
			i++
		default:
			if !utf8.FullRune(data[i:]) {
				g.pending = append([]byte(nil), data[i:]...)
				return
			}
			r, n := utf8.DecodeRune(data[i:])
			i += n
			g.put(r)
		}
	}
}

func (g *termGrid) put(r rune) {
	width := ansi.StringWidth(string(r))
	if width == 0 {
		return
	}
	if g.col+width > g.w {
		g.wrapped++
		g.col = 0
		g.lineFeed()
	}
	for k := range width {
		g.cells[g.row][g.col+k] = termCell{ch: r}
	}
	g.col += width
}

// escape consumes one escape sequence and reports its length, or false when
// the sequence is not all here yet.
func (g *termGrid) escape(data []byte) (int, bool) {
	if len(data) < 2 {
		return 0, false
	}
	switch data[1] {
	case '7':
		g.srow, g.scol = g.row, g.col
		return 2, true
	case '8':
		g.row, g.col = g.srow, g.scol
		return 2, true
	case '[':
		end := 2
		for end < len(data) && (data[end] < 0x40 || data[end] > 0x7e) {
			end++
		}
		if end >= len(data) {
			return 0, false
		}
		g.csi(string(data[2:end]), data[end])
		return end + 1, true
	case ']':
		end := bytes.IndexByte(data, '\a')
		if end < 0 {
			return 0, false
		}
		g.osc(string(data[2:end]))
		return end + 1, true
	case '_':
		end := bytes.Index(data, []byte("\x1b\\"))
		if end < 0 {
			return 0, false
		}
		g.apc(string(data[2:end]))
		return end + 2, true
	default:
		return 2, true
	}
}

func (g *termGrid) csi(params string, final byte) {
	if strings.HasPrefix(params, "?") {
		return
	}
	var nums []int
	for _, part := range strings.Split(params, ";") {
		n, _ := strconv.Atoi(part)
		nums = append(nums, n)
	}
	arg := func(i, fallback int) int {
		if i < len(nums) && nums[i] != 0 {
			return nums[i]
		}
		return fallback
	}
	switch final {
	case 'H':
		g.row = min(max(arg(0, 1)-1, 0), g.h-1)
		g.col = min(max(arg(1, 1)-1, 0), g.w-1)
	case 'G':
		g.col = min(max(arg(0, 1)-1, 0), g.w-1)
	case 'A':
		g.row = max(g.row-arg(0, 1), 0)
	case 'B':
		g.row = min(g.row+arg(0, 1), g.h-1)
	case 'X':
		g.erase(g.row, g.col, g.col+arg(0, 1))
	case 'K':
		if arg(0, 0) == 0 {
			g.erase(g.row, g.col, g.w)
		}
	case 'J':
		switch arg(0, 0) {
		case 0:
			g.erase(g.row, g.col, g.w)
			for r := g.row + 1; r < g.h; r++ {
				g.erase(r, 0, g.w)
			}
		case 2:
			for r := range g.h {
				g.erase(r, 0, g.w)
			}
			g.kitty = map[int]termImage{}
		}
	case 'm':
		switch {
		case params == "" || params == "0":
			g.sgr = ""
		case strings.HasPrefix(params, "0;"):
			g.sgr = params
		default:
			g.sgr += ";" + params
		}
	}
}

// osc handles the iTerm inline image. Every other OSC has no effect on cells.
func (g *termGrid) osc(body string) {
	if !strings.HasPrefix(body, "1337;File=") {
		return
	}
	head, payload, _ := strings.Cut(body, ":")
	var cols, rows int
	for _, field := range strings.Split(head, ";") {
		if value, ok := strings.CutPrefix(field, "width="); ok {
			cols, _ = strconv.Atoi(value)
		}
		if value, ok := strings.CutPrefix(field, "height="); ok {
			rows, _ = strconv.Atoi(value)
		}
	}
	if png, err := base64.StdEncoding.DecodeString(payload); err == nil {
		cols, rows = itermCells(png, cols, rows)
	}
	tag := termTag(payload, g.row, g.col)
	for r := g.row; r < min(g.row+rows, g.h); r++ {
		for c := g.col; c < min(g.col+cols, g.w); c++ {
			g.cells[r][c] = termCell{image: tag}
		}
	}
}

// itermCells is the part of a cols by rows box an iTerm image covers. iTerm
// scales the picture to fit and keeps its aspect, so a picture narrower or
// shorter than the box leaves the rest of the box as it was.
func itermCells(png []byte, cols, rows int) (int, int) {
	w, h, ok := pngPixelSize(png)
	if !ok || cols < 1 || rows < 1 {
		return cols, rows
	}
	boxW, boxH := cols*exploreCellWidth, rows*exploreCellHeight
	if w*boxH > h*boxW {
		scaled := h * boxW / w
		return cols, min(rows, max((scaled+exploreCellHeight-1)/exploreCellHeight, 1))
	}
	scaled := w * boxH / h
	return min(cols, max((scaled+exploreCellWidth-1)/exploreCellWidth, 1)), rows
}

func (g *termGrid) apc(body string) {
	if !strings.HasPrefix(body, "G") {
		return
	}
	control, payload, _ := strings.Cut(body[1:], ";")
	fields := map[string]string{}
	for _, part := range strings.Split(control, ",") {
		if key, value, ok := strings.Cut(part, "="); ok {
			fields[key] = value
		}
	}
	id, _ := strconv.Atoi(fields["i"])
	switch fields["a"] {
	case "d":
		delete(g.kitty, id)
	case "T":
		cols, _ := strconv.Atoi(fields["c"])
		rows, _ := strconv.Atoi(fields["r"])
		g.kitty[id] = termImage{tag: termTag(payload, g.row, g.col), row: g.row, col: g.col, cols: cols, rows: rows}
	}
}

// imageCells maps every cell that shows image pixels to the paint it shows.
func (g *termGrid) imageCells() map[[2]int]string {
	out := map[[2]int]string{}
	for r := range g.cells {
		for c, cell := range g.cells[r] {
			if cell.image != "" {
				out[[2]int{r, c}] = cell.image
			}
		}
	}
	for _, img := range g.kitty {
		for r := img.row; r < min(img.row+img.rows, g.h); r++ {
			for c := img.col; c < min(img.col+img.cols, g.w); c++ {
				out[[2]int{r, c}] = img.tag
			}
		}
	}
	return out
}

// rowText is the text on a zero-based row. Image cells print as '#'.
func (g *termGrid) rowText(row int) string {
	images := g.imageCells()
	var b strings.Builder
	for c, cell := range g.cells[row] {
		switch _, pictured := images[[2]int{row, c}]; {
		case pictured:
			b.WriteByte('#')
		case cell.ch == 0:
			b.WriteByte(' ')
		default:
			b.WriteRune(cell.ch)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func (g *termGrid) String() string {
	var b strings.Builder
	for r := range g.h {
		fmt.Fprintf(&b, "%2d|%s\n", r+1, g.rowText(r))
	}
	return b.String()
}

// exploreScreen runs an explorer model under Bubble Tea's real renderer and
// feeds what the renderer writes into a termGrid.
//
// Each step is rendered and flushed before the next begins, so the renderer
// diffs every frame against the one before it; that is the worst case for
// rows it decides to skip. Commands the model returns are not run. A replay
// arrives when the test says so, through settle, which makes each frame
// deterministic and lets a test look at the frame between a focus change and
// the replay that follows it.
//
// With burst set, send returns as soon as the model has handled the message.
// The renderer then draws whichever frames its timer happens to catch, as it
// does when several messages arrive between two renders.
type exploreScreen struct {
	t     *testing.T
	prog  *tea.Program
	out   *screenOutput
	grid  *termGrid
	done  chan struct{}
	step  int
	burst bool
}

type screenOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *screenOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

// takeThrough removes and returns the output up to the end of the write that
// carries marker, or nil while that write has not happened.
func (o *screenOutput) takeThrough(marker string) []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !bytes.Contains(o.buf.Bytes(), []byte(marker)) {
		return nil
	}
	out := append([]byte(nil), o.buf.Bytes()...)
	o.buf.Reset()
	return out
}

type screenStep struct {
	id    int
	msg   tea.Msg
	reply chan exploreModel
}

// screenSettle delivers the focused replay, as a finished decode would.
type screenSettle struct{}

// screenApply changes the model in one step, however many messages that
// stands for, so the renderer never sees the frames in between.
type screenApply func(exploreModel) exploreModel

type screenHost struct {
	m    exploreModel
	step int
}

func (h screenHost) Init() tea.Cmd { return nil }

func (h screenHost) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	step, ok := msg.(screenStep)
	if !ok {
		// Bubble Tea's own messages, such as the size, reach the model too.
		next, _ := h.m.Update(msg)
		h.m = next.(exploreModel)
		return h, nil
	}
	switch inner := step.msg.(type) {
	case nil:
	case screenSettle:
		h.m = settleReplay(h.m)
	case screenApply:
		h.m = inner(h.m)
	default:
		next, _ := h.m.Update(inner)
		h.m = next.(exploreModel)
	}
	h.step = step.id
	step.reply <- h.m
	return h, nil
}

// View marks the header row with the step so the test can tell when this
// frame has been flushed. The title sequence has no width and the header is
// never under a replay.
func (h screenHost) View() string {
	return fmt.Sprintf("\x1b]0;frame %d\a", h.step) + h.m.View()
}

// settleReplay hands the model the poster its last schedulePoster asked for.
func settleReplay(m exploreModel) exploreModel {
	item, ok := m.focused()
	if !ok || item.Replay == "" || m.posterFor != item.Path || (m.poster != nil && m.mediaFor == item.Path) {
		return m
	}
	w, h := m.stagePixels()
	next, _ := m.Update(explorePoster{gen: m.loadGen, item: item.Path, poster: posterFor(item.Path, w, h)})
	return next.(exploreModel)
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
	png, err := explore.EncodePNG(img)
	if err != nil {
		panic(err)
	}
	return png
}

func openExploreScreen(t *testing.T, m exploreModel) *exploreScreen {
	t.Helper()
	s := &exploreScreen{t: t, out: &screenOutput{}, grid: newTermGrid(m.width, m.height), done: make(chan struct{})}
	s.prog = tea.NewProgram(screenHost{m: m},
		tea.WithInput(bytes.NewReader(nil)), tea.WithOutput(s.out),
		tea.WithAltScreen(), tea.WithoutSignalHandler(), tea.WithFPS(120))
	go func() {
		defer close(s.done)
		_, _ = s.prog.Run()
	}()
	t.Cleanup(s.close)
	// The renderer learns the terminal size from this message.
	s.prog.Send(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	s.send(nil)
	return s
}

// send delivers one message, waits until its frame has reached the grid, and
// returns the model that frame was drawn from.
func (s *exploreScreen) send(msg tea.Msg) exploreModel {
	s.t.Helper()
	s.step++
	reply := make(chan exploreModel, 1)
	s.prog.Send(screenStep{id: s.step, msg: msg, reply: reply})
	var m exploreModel
	select {
	case m = <-reply:
	case <-s.done:
		s.t.Fatal("the program stopped")
	case <-time.After(10 * time.Second):
		s.t.Fatal("the program did not handle the step")
	}
	if s.burst {
		return m
	}
	marker := fmt.Sprintf("\x1b]0;frame %d\a", s.step)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if data := s.out.takeThrough(marker); data != nil {
			_, _ = s.grid.Write(data)
			return m
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("frame %d was never flushed", s.step)
		}
		time.Sleep(time.Millisecond)
	}
}

// key sends a key by name and then the replay the new focus asked for.
func (s *exploreScreen) key(name string) exploreModel {
	s.t.Helper()
	s.send(screenKey(name))
	return s.send(screenSettle{})
}

func (s *exploreScreen) close() {
	s.prog.Quit()
	<-s.done
}

func screenKey(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

var (
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

// drawnBox is the cells a view's replay covers on the terminal: the Kitty
// placement as asked for, or the part of the iTerm box the picture fills.
func drawnBox(m exploreModel) (row, col, cols, rows int, ok bool) {
	row, col, cols, rows, ok = imageBox(m.View())
	if ok && m.protocol == explore.ProtocolITerm {
		cols, rows = itermCells(m.poster, cols, rows)
	}
	return row, col, cols, rows, ok
}

// viewText strips image payloads and control sequences from one view line.
func viewText(line string) string {
	line = viewKittyAPC.ReplaceAllString(line, "")
	line = viewITermOSC.ReplaceAllString(line, "")
	line = strings.NewReplacer("\x1b7", "", "\x1b8", "").Replace(line)
	return viewCSI.ReplaceAllString(line, "")
}

// checkScreen compares what the grid holds with what the model asked for and
// returns one line per difference: image cells left where no image belongs,
// image cells missing from where it belongs, two paints sharing the box, a
// row that wrapped, or a screen that scrolled.
func checkScreen(g *termGrid, m exploreModel) []string {
	want := map[[2]int]bool{}
	if row, col, cols, rows, ok := drawnBox(m); ok {
		for r := row - 1; r < row-1+rows; r++ {
			for c := col - 1; c < col-1+cols; c++ {
				want[[2]int{r, c}] = true
			}
		}
	}
	stale, missing := map[int]bool{}, map[int]bool{}
	paints := map[string]bool{}
	have := g.imageCells()
	for at, tag := range have {
		if want[at] {
			paints[tag] = true
		} else {
			stale[at[0]+1] = true
		}
	}
	for at := range want {
		if _, ok := have[at]; !ok {
			missing[at[0]+1] = true
		}
	}
	var problems []string
	if len(stale) > 0 {
		problems = append(problems, "image left on rows "+rowSpan(stale))
	}
	if len(missing) > 0 {
		problems = append(problems, "image missing from rows "+rowSpan(missing))
	}
	if len(paints) > 1 {
		problems = append(problems, fmt.Sprintf("%d different paints inside the image box", len(paints)))
	}
	if g.wrapped > 0 {
		problems = append(problems, fmt.Sprintf("%d rows wrapped past the right margin", g.wrapped))
	}
	if g.scrolled > 0 {
		problems = append(problems, fmt.Sprintf("the screen scrolled %d times", g.scrolled))
	}
	return problems
}

func rowSpan(rows map[int]bool) string {
	lo, hi := 0, 0
	for row := range rows {
		if lo == 0 || row < lo {
			lo = row
		}
		hi = max(hi, row)
	}
	return fmt.Sprintf("%d-%d", lo, hi)
}

// screenFeed is a feed of n festivals, two a day. Every third has no replay
// and every fifth is archived, so moving focus and changing the status lens
// both pass through rows with and without an image.
func screenFeed(t *testing.T, w, h int, protocol string, n int) exploreModel {
	t.Helper()
	items := make([]explore.Item, n)
	for i := range items {
		day := time.Date(2026, time.March, 28-i/2, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		path := fmt.Sprintf("festivals/.dungeon/completed/%s/item-%02d", day, i)
		items[i] = explore.Item{
			DoneDate: day, DateSource: explore.DateBucket, Status: "completed",
			DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: explore.KindFestival,
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
		query:   explore.Query{Status: "finished", Dungeon: explore.LensAll},
		index: explore.Index{
			Dungeons: []explore.DungeonPrint{{Path: "festivals/.dungeon", Label: "Festivals"}},
			Items:    items,
			BuiltAt:  time.Date(2026, time.March, 29, 0, 0, 0, 0, time.UTC),
		},
	}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	return m
}
