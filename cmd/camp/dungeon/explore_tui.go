package dungeon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/obey-shared/brand"
)

const (
	exploreImageID       = 1
	exploreReadingStatus = "Reading dungeons…"
)

type exploreStatusSource uint8

const (
	exploreActionStatus exploreStatusSource = iota
	// exploreReadStatus is a document that could not be opened. It belongs to
	// one entry, so it goes when another entry is opened or focused.
	exploreReadStatus
	exploreQueryStatus
	exploreLoadStatus
	exploreReplayStatus
)

// exploreNote is one line for the status row and whether it reports a failure.
type exploreNote struct {
	text   string
	failed bool
}

type exploreModel struct {
	ctx      context.Context
	root     string
	cacheDir string
	query    explore.Query
	index    explore.Index
	visible  explore.Result
	cursor   int
	width    int
	height   int

	loading    bool
	loadFailed bool
	sized      bool
	// status answers one key press: a copy, a jump that needs shell
	// integration, a document that would not open. The notes are state. Each
	// is set and cleared by its own source, and the status row falls back to
	// them when the one-shot status goes (statusRow).
	status     string
	statusErr  bool
	queryNote  exploreNote
	loadNote   exploreNote
	replayNote exploreNote
	indexGen   int
	help       bool
	filtering  bool
	filter     string
	plain      bool
	styles     exploreStyles

	reading bool
	reader  exploreReader

	protocol string
	reduced  bool
	loadGen  int
	mediaFor string
	poster   []byte
	frames   [][]byte
	delays   []time.Duration
	frame    int
	playing  bool
	tickGen  int

	// posterFor and posterReplay name the item and replay file the last
	// schedulePoster asked for, drawn or still decoding.
	posterFor    string
	posterReplay string

	decode       func(context.Context, string, int, int) (explore.Frames, error)
	decodeCancel context.CancelFunc

	gotoEnabled bool
	gotoPath    string
	quitting    bool

	// fresh says the index was built for this start. Init saves it and does
	// not read the camp again.
	fresh bool
}

type exploreLoaded struct {
	gen     int
	index   explore.Index
	changed bool
	err     error
}

type explorePoster struct {
	gen     int
	item    string
	poster  []byte
	frames  [][]byte
	delays  []time.Duration
	tooLong bool
	cut     bool
	err     error
}

type exploreTick struct{ gen int }

type exploreDecode struct{ gen int }

// runExploreTUI runs the feed. start is an index built for this run, or nil
// to paint from the cache and then read the camp.
func runExploreTUI(cmd *cobra.Command, root, cacheDir string, query explore.Query, protocol, pathOutput string, plain bool, start *explore.Index) error {
	ctx := cmd.Context()
	model := exploreModel{
		ctx:         ctx,
		root:        root,
		cacheDir:    cacheDir,
		query:       query,
		width:       80,
		height:      24,
		loading:     true,
		protocol:    protocol,
		decode:      explore.DecodeReplay,
		plain:       plain,
		styles:      newExploreStyles(plain),
		reduced:     brand.ReducedMotion(),
		gotoEnabled: pathOutput != "",
	}
	if start != nil {
		model.index = *start
		model.loading = false
		model.fresh = true
		_ = model.applyQuery()
	} else if cacheDir != "" {
		if idx, ok, _ := explore.LoadCache(ctx, cacheDir); ok {
			model.index = idx
			model.loading = false
			_ = model.applyQuery()
		}
	}
	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen())
	signals := watchExploreSignals()
	final, err := prog.Run()
	stopped := signals.stop()
	done, ok := final.(exploreModel)
	if ok {
		done.cancelDecode()
	}
	if exit := exploreSignalExit(cmd.CommandPath(), stopped, err); exit != nil {
		return exit
	}
	if err != nil {
		return camperrors.Wrap(err, "running dungeon explorer")
	}
	if !ok || pathOutput == "" || done.gotoPath == "" {
		return nil
	}
	if err := os.WriteFile(pathOutput, []byte(done.gotoPath), 0o600); err != nil {
		return camperrors.Wrap(err, "writing selected dungeon path")
	}
	return nil
}

func (m exploreModel) Init() tea.Cmd {
	if m.fresh {
		return m.saveCacheCmd()
	}
	return m.loadCmd()
}

func (m exploreModel) loadCmd() tea.Cmd {
	root := m.root
	cached := m.index
	haveCache := !m.loading
	ctx := m.ctx
	gen := m.indexGen
	return func() tea.Msg {
		if haveCache {
			idx, changed, err := explore.Refresh(ctx, root, cached)
			return exploreLoaded{gen: gen, index: idx, changed: changed, err: err}
		}
		idx, err := explore.Build(ctx, root)
		return exploreLoaded{gen: gen, index: idx, changed: true, err: err}
	}
}

// saveCacheCmd writes the index off the update loop. A failed write costs
// only the next start's first paint, so it is not reported.
func (m exploreModel) saveCacheCmd() tea.Cmd {
	if m.cacheDir == "" {
		return nil
	}
	ctx, dir, idx := m.ctx, m.cacheDir, m.index
	return func() tea.Msg {
		_ = explore.SaveCache(ctx, dir, idx)
		return nil
	}
}

func (m exploreModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		rows := m.stageRows()
		pixW, pixH := m.stagePixels()
		first := !m.sized
		m.sized = true
		m.width, m.height = msg.Width, msg.Height
		m.reflowReader()
		newW, newH := m.stagePixels()
		if m.stageRows() == rows && newW == pixW && newH == pixH {
			if first {
				// The model starts at 80x24, so a terminal of that size
				// reports no change. Its focused replay is still unrequested.
				m, cmd := m.ensurePoster()
				return m, cmd
			}
			return m, nil
		}
		m, cmd := m.schedulePoster()
		return m, cmd
	case exploreLoaded:
		if msg.gen != m.indexGen {
			return m, nil
		}
		m.loading = false
		m.loadFailed = msg.err != nil
		if msg.err != nil {
			m.setStatus(exploreLoadStatus, msg.err.Error(), true)
			// The index on screen is still the one being browsed.
			m, cmd := m.ensurePoster()
			return m, cmd
		}
		m.setStatus(exploreLoadStatus, "", false)
		selected, hadSelection := m.focused()
		var save tea.Cmd
		if msg.changed {
			m.index = msg.index
			save = m.saveCacheCmd()
		}
		if m.applyQueryFollowing(selected, hadSelection) {
			// A status about the row that was focused does not follow the
			// cursor to another one.
			m.clearActionStatus()
		}
		m, cmd := m.ensurePoster()
		return m, tea.Batch(save, cmd)
	case exploreDecode:
		if msg.gen != m.loadGen {
			return m, nil
		}
		cmd := m.startDecode()
		return m, cmd
	case explorePoster:
		if msg.gen != m.loadGen {
			return m, nil
		}
		m.cancelDecode()
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.poster = msg.poster
		m.frames = msg.frames
		m.delays = msg.delays
		m.frame = 0
		m.mediaFor = msg.item
		m.playing = len(msg.frames) > 1 && !m.reduced && msg.err == nil
		switch {
		case msg.cut:
			m.setStatus(exploreReplayStatus, "The replay file is incomplete. Showing a still.", false)
		case msg.tooLong || errors.Is(msg.err, explore.ErrReplayTooLong):
			m.setStatus(exploreReplayStatus, "Replay is too long to play here.", false)
		case msg.err != nil:
			m.setStatus(exploreReplayStatus, "The replay could not be read.", true)
		default:
			m.setStatus(exploreReplayStatus, "", false)
		}
		if m.playing {
			m.poster = m.frames[0]
			return m, m.tick()
		}
		return m, nil
	case exploreTick:
		if msg.gen != m.tickGen || !m.playing || len(m.frames) == 0 {
			return m, nil
		}
		m.frame = (m.frame + 1) % len(m.frames)
		m.poster = m.frames[m.frame]
		return m, m.tick()
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m exploreModel) onKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.onFilterKey(key)
	}
	if m.reading {
		return m.onReaderKey(key)
	}
	switch key.String() {
	case "ctrl+c", "q":
		return m.quit()
	case "esc":
		if m.help {
			m.help = false
			return m, nil
		}
		return m.quit()
	case "up", "k":
		return m.move(-1)
	case "down", "j":
		return m.move(1)
	case "ctrl+u":
		return m.move(-m.page())
	case "ctrl+d":
		return m.move(m.page())
	case "left", "[":
		return m.requery(func(q *explore.Query) {
			q.Dungeon = explore.StepDungeon(m.index.Items, m.query.Status, m.visible.DungeonLens, -1)
		})
	case "right", "]":
		return m.requery(func(q *explore.Query) {
			q.Dungeon = explore.StepDungeon(m.index.Items, m.query.Status, m.visible.DungeonLens, 1)
		})
	case "s":
		return m.requery(func(q *explore.Query) {
			q.Status = explore.NextPreset(m.index.Items, m.query.Status)
		})
	case "/":
		m.filtering = true
		m.filter = m.query.Text
		return m, nil
	case " ":
		return m.togglePlay()
	case "enter":
		return m.openReader()
	case "g":
		return m.hop()
	case "y":
		return m.copyPath(m.focusedPath())
	case "r":
		if m.loading {
			return m, nil
		}
		m.loading = true
		m.indexGen++
		// A rescan rereads the camp, the focused replay included.
		m.posterFor = ""
		m.setStatus(exploreLoadStatus, exploreReadingStatus, false)
		return m, m.loadCmd()
	case "?":
		m.help = !m.help
		return m, nil
	}
	return m, nil
}

func (m exploreModel) onFilterKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Type == tea.KeyRunes && !key.Alt:
		m.filter += filterText(key.Runes)
		return m, nil
	case key.Type == tea.KeySpace:
		m.filter += " "
		return m, nil
	}
	switch key.String() {
	case "ctrl+c":
		// ctrl+c leaves the explorer from every mode.
		return m.quit()
	case "esc":
		m.filtering = false
		return m, nil
	case "enter":
		m.filtering = false
		text := strings.TrimSpace(m.filter)
		return m.requery(func(q *explore.Query) { q.Text = text })
	case "backspace":
		if m.filter != "" {
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
		}
		return m, nil
	}
	return m, nil
}

func filterText(runes []rune) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, string(runes))
}

func (m exploreModel) move(delta int) (tea.Model, tea.Cmd) {
	// A status reports one key press. The next movement key clears it even
	// when the cursor has nowhere to go, as on a feed of one row.
	m.clearActionStatus()
	if len(m.visible.Items) == 0 {
		return m, nil
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.visible.Items) {
		next = len(m.visible.Items) - 1
	}
	if next == m.cursor {
		return m, nil
	}
	m.cursor = next
	m, cmd := m.schedulePoster()
	return m, cmd
}

func (m exploreModel) page() int {
	n := m.bodyHeight() / 2
	if n < 1 {
		return 1
	}
	return n
}

func (m *exploreModel) applyQuery() error {
	result, err := explore.Apply(m.index, m.query)
	if err != nil {
		// Rows from the previous lens must not remain actionable when the
		// current query no longer resolves against the refreshed index.
		m.visible = explore.Result{}
		m.cursor = 0
		m.setStatus(exploreQueryStatus, err.Error(), true)
		return err
	}
	m.setStatus(exploreQueryStatus, "", false)
	m.visible = result
	m.query.Status = result.StatusLens
	// Keep the supplied dungeon selector so a later refresh can resolve that label.
	if m.cursor >= len(result.Items) {
		m.cursor = 0
	}
	if len(result.Items) == 0 {
		m.cursor = 0
	}
	return nil
}

// applyQueryFollowing applies the query and keeps the cursor on the item that
// was focused, wherever the new rows put it. A refresh, a lens, and a filter
// may all insert, drop, or reorder rows; the old position would land on an
// unrelated one. If the item is gone the cursor starts at the first row.
// It reports whether the focused item changed.
func (m *exploreModel) applyQueryFollowing(selected explore.Item, hadSelection bool) bool {
	_ = m.applyQuery()
	m.cursor = 0
	if hadSelection {
		for i, item := range m.visible.Items {
			if item.Path == selected.Path {
				m.cursor = i
				break
			}
		}
	}
	now, ok := m.focused()
	return !ok || !hadSelection || now.Path != selected.Path
}

// requery changes the lens or the filter. The replay of a row that stays
// focused is kept, not decoded again.
func (m exploreModel) requery(change func(*explore.Query)) (tea.Model, tea.Cmd) {
	m.clearActionStatus()
	selected, hadSelection := m.focused()
	change(&m.query)
	m.applyQueryFollowing(selected, hadSelection)
	m, cmd := m.ensurePoster()
	return m, cmd
}

func (m exploreModel) schedulePoster() (exploreModel, tea.Cmd) {
	m.setStatus(exploreReplayStatus, "", false)
	m.loadGen++
	m.tickGen++
	m.cancelDecode()
	m.poster = nil
	m.frames = nil
	m.playing = false
	m.mediaFor = ""
	m.posterFor, m.posterReplay = "", ""
	if m.protocol == explore.ProtocolOff || m.stageRows() == 0 {
		return m, nil
	}
	item, ok := m.focused()
	if !ok || item.Replay == "" {
		return m, nil
	}
	m.posterFor, m.posterReplay = item.Path, item.Replay
	gen := m.loadGen
	return m, tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return exploreDecode{gen: gen}
	})
}

// ensurePoster asks for the focused replay unless that replay is already
// drawn or decoding. A refresh that leaves the focused row alone must not
// blank the replay and start it again.
func (m exploreModel) ensurePoster() (exploreModel, tea.Cmd) {
	if item, ok := m.focused(); ok && m.posterFor != "" && m.posterFor == item.Path && m.posterReplay == item.Replay {
		return m, nil
	}
	return m.schedulePoster()
}

func (m exploreModel) decodeCmd(ctx context.Context) tea.Cmd {
	item, ok := m.focused()
	if !ok || item.Replay == "" {
		return nil
	}
	decode := m.decode
	if decode == nil {
		decode = explore.DecodeReplay
	}
	gen := m.loadGen
	rel := item.Path
	path := filepath.Join(m.root, filepath.FromSlash(item.Replay))
	maxW, maxH := m.stagePixels()
	return func() tea.Msg {
		frames, err := decode(ctx, path, maxW, maxH)
		msg := explorePoster{
			gen:     gen,
			item:    rel,
			tooLong: errors.Is(err, explore.ErrReplayTooLong),
			cut:     errors.Is(err, explore.ErrReplayIncomplete),
		}
		if err != nil && !msg.tooLong && !msg.cut {
			msg.err = err
			return msg
		}
		if err := ctx.Err(); err != nil {
			msg.err = err
			return msg
		}
		if frames.Poster != nil {
			png, encErr := explore.EncodePNG(frames.Poster)
			if encErr != nil {
				msg.err = encErr
				return msg
			}
			msg.poster = png
		}
		if len(frames.Frames) > 1 && !m.reduced {
			msg.frames = make([][]byte, len(frames.Frames))
			msg.delays = frames.Delays
			for i, frame := range frames.Frames {
				if err := ctx.Err(); err != nil {
					msg.err = err
					msg.frames = nil
					return msg
				}
				png, encErr := explore.EncodePNG(frame)
				if encErr != nil {
					msg.err = encErr
					msg.frames = nil
					return msg
				}
				msg.frames[i] = png
			}
		}
		return msg
	}
}

func (m exploreModel) tick() tea.Cmd {
	if !m.playing || len(m.delays) == 0 {
		return nil
	}
	delay := m.delays[m.frame]
	gen := m.tickGen
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return exploreTick{gen: gen}
	})
}

func (m exploreModel) togglePlay() (tea.Model, tea.Cmd) {
	if m.reduced || len(m.frames) < 2 {
		return m, nil
	}
	m.playing = !m.playing
	m.tickGen++
	if m.playing {
		return m, m.tick()
	}
	return m, nil
}

func (m exploreModel) focused() (explore.Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible.Items) {
		return explore.Item{}, false
	}
	return m.visible.Items[m.cursor], true
}

func (m exploreModel) focusedPath() string {
	item, ok := m.focused()
	if !ok {
		return ""
	}
	return joinAbs(m.root, item.Path)
}

func (m exploreModel) hop() (tea.Model, tea.Cmd) {
	item, ok := m.focused()
	if !ok {
		return m, nil
	}
	return m.hopTo(item)
}

func (m exploreModel) hopTo(item explore.Item) (tea.Model, tea.Cmd) {
	if !m.gotoEnabled {
		m.setStatus(exploreActionStatus, `go needs shell integration: eval "$(camp shell-init <shell>)"`, true)
		return m, nil
	}
	m.gotoPath = exploreJump(m.root, item)
	return m.quit()
}

func (m exploreModel) quit() (tea.Model, tea.Cmd) {
	m.cancelDecode()
	m.quitting = true
	return m, tea.Quit
}

func (m *exploreModel) cancelDecode() {
	if m.decodeCancel != nil {
		m.decodeCancel()
		m.decodeCancel = nil
	}
}

func (m *exploreModel) startDecode() tea.Cmd {
	m.cancelDecode()
	base := m.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	cmd := m.decodeCmd(ctx)
	if cmd == nil {
		cancel()
		return nil
	}
	m.decodeCancel = cancel
	return cmd
}

func (m exploreModel) copyPath(path string) (tea.Model, tea.Cmd) {
	if path == "" {
		return m, nil
	}
	if err := copyExplorePath(path); err != nil {
		m.setStatus(exploreActionStatus, "copy failed: "+err.Error(), true)
		return m, nil
	}
	m.setStatus(exploreActionStatus, "copied", false)
	return m, nil
}

// clearActionStatus drops a status that answers one key press: a copy, a
// jump that needs shell integration, or a document that would not open. It
// goes with the next key that moves focus, changes the lens or filter, or
// opens, scrolls, or closes the reader. What it covered on the status row,
// a query, load, or replay note, shows again.
func (m *exploreModel) clearActionStatus() {
	m.status, m.statusErr = "", false
}

// setStatus records text from source. Query, load, and replay notes describe
// state and each keeps its own line; anything else is the one-shot status.
func (m *exploreModel) setStatus(source exploreStatusSource, text string, failed bool) {
	note := exploreNote{text: text, failed: failed && text != ""}
	switch source {
	case exploreQueryStatus:
		m.queryNote = note
	case exploreLoadStatus:
		m.loadNote = note
	case exploreReplayStatus:
		m.replayNote = note
	default:
		m.status, m.statusErr = note.text, note.failed
	}
}
