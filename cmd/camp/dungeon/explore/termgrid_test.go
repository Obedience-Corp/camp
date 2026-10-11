package explore

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// termGrid is the part of a terminal the explorer depends on: a grid of
// cells, a cursor, autowrap, scrolling, tab stops, the erase commands Bubble
// Tea's renderer sends, and the two image protocols. An iTerm image lives in
// the cells it covers and goes when they are written or erased; it keeps its
// aspect, so it covers only the part of the requested box the picture fills.
// A Kitty image is a separate layer that only a delete command removes.
//
// By default every code point takes its own cells, as on a terminal that does
// not join emoji sequences: the widest a row can come out. With joins set a
// grapheme cluster is one glyph, as Kitty, Ghostty, WezTerm, and iTerm draw
// it: a ZWJ sequence or a flag takes two cells, so the row comes out shorter
// than its code points, and a keycap takes two where its code points take one.
//
// Tests assert on what the grid holds after the real renderer has written
// to it, which is what a person would see, not on the strings View returns.
type termGrid struct {
	w, h       int
	joins      bool
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
			if g.joins {
				cluster, width := ansi.FirstGraphemeCluster(data[i:], ansi.GraphemeWidth)
				i += max(len(cluster), n)
				g.draw(r, width)
				continue
			}
			i += n
			g.draw(r, ansi.StringWidth(string(r)))
		}
	}
}

// draw puts one glyph of width cells at the cursor. Every cell of it holds r.
func (g *termGrid) draw(r rune, width int) {
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
