package explore

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

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

func openExploreScreen(t *testing.T, m exploreModel) *exploreScreen {
	t.Helper()
	return openExploreScreenOn(t, m, newTermGrid(m.width, m.height))
}

// openJoiningScreen is openExploreScreen on a terminal that joins emoji
// sequences into one glyph.
func openJoiningScreen(t *testing.T, m exploreModel) *exploreScreen {
	t.Helper()
	grid := newTermGrid(m.width, m.height)
	grid.joins = true
	return openExploreScreenOn(t, m, grid)
}

// screenKinds are the two ways a terminal draws an emoji sequence. A layout
// has to hold on both: rows must not run past the margin where the code
// points are drawn apart, and columns must still line up where they are
// joined and the row comes out shorter.
var screenKinds = []struct {
	name string
	open func(*testing.T, exploreModel) *exploreScreen
}{
	{"sequences apart", openExploreScreen},
	{"sequences joined", openJoiningScreen},
}

func openExploreScreenOn(t *testing.T, m exploreModel, grid *termGrid) *exploreScreen {
	t.Helper()
	s := &exploreScreen{t: t, out: &screenOutput{}, grid: grid, done: make(chan struct{})}
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

// drawnBox is the cells a view's replay covers on the terminal: the Kitty
// placement as asked for, or the part of the iTerm box the picture fills.
func drawnBox(m exploreModel) (row, col, cols, rows int, ok bool) {
	row, col, cols, rows, ok = imageBox(m.View())
	if ok && m.protocol == intexplore.ProtocolITerm {
		cols, rows = itermCells(m.poster, cols, rows)
	}
	return row, col, cols, rows, ok
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
