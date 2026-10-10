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

	loading   bool
	status    string
	statusErr bool
	help      bool
	filtering bool
	filter    string
	plain     bool
	styles    exploreStyles

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

	decode       func(context.Context, string, int, int) (explore.Frames, error)
	decodeCancel context.CancelFunc

	gotoEnabled bool
	gotoPath    string
	quitting    bool
}

type exploreLoaded struct {
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
	err     error
}

type exploreTick struct{ gen int }

type exploreDecode struct{ gen int }

func runExploreTUI(cmd *cobra.Command, root, cacheDir string, query explore.Query, protocol, pathOutput string, plain bool) error {
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
	if cacheDir != "" {
		if idx, ok, _ := explore.LoadCache(cacheDir); ok {
			model.index = idx
			model.loading = false
			if err := model.applyQuery(); err != nil {
				model.statusErr = true
				model.status = err.Error()
			}
		}
	}
	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen())
	final, err := prog.Run()
	done, ok := final.(exploreModel)
	if ok {
		done.cancelDecode()
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
	return m.loadCmd()
}

func (m exploreModel) loadCmd() tea.Cmd {
	root := m.root
	cached := m.index
	haveCache := !m.loading
	ctx := m.ctx
	return func() tea.Msg {
		if haveCache {
			idx, changed, err := explore.Refresh(ctx, root, cached)
			return exploreLoaded{index: idx, changed: changed, err: err}
		}
		idx, err := explore.Build(ctx, root)
		return exploreLoaded{index: idx, changed: true, err: err}
	}
}

func (m exploreModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		rows := m.stageRows()
		pixW, pixH := m.stagePixels()
		m.width, m.height = msg.Width, msg.Height
		m.reflowReader()
		newW, newH := m.stagePixels()
		if m.stageRows() == rows && newW == pixW && newH == pixH {
			return m, nil
		}
		m, cmd := m.schedulePoster()
		return m, cmd
	case exploreLoaded:
		m.loading = false
		if msg.err != nil {
			m.statusErr = true
			m.status = msg.err.Error()
			return m, nil
		}
		if m.status == exploreReadingStatus {
			m.status = ""
		}
		if msg.changed {
			m.index = msg.index
			if m.cacheDir != "" {
				_ = explore.SaveCache(m.cacheDir, m.index)
			}
		}
		if err := m.applyQuery(); err != nil {
			m.statusErr = true
			m.status = err.Error()
		}
		m, cmd := m.schedulePoster()
		return m, cmd
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
		if msg.tooLong || errors.Is(msg.err, explore.ErrReplayTooLong) {
			m.statusErr = false
			m.status = "Replay is too long to play here."
		} else if msg.err != nil {
			m.statusErr = true
			m.status = "The replay could not be read."
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
		m.query.Dungeon = explore.StepDungeon(m.index.Items, m.query.Status, m.visible.DungeonLens, -1)
		_ = m.applyQuery()
		m, cmd := m.schedulePoster()
		return m, cmd
	case "right", "]":
		m.query.Dungeon = explore.StepDungeon(m.index.Items, m.query.Status, m.visible.DungeonLens, 1)
		_ = m.applyQuery()
		m, cmd := m.schedulePoster()
		return m, cmd
	case "s":
		m.query.Status = explore.NextPreset(m.index.Items, m.query.Status)
		_ = m.applyQuery()
		m, cmd := m.schedulePoster()
		return m, cmd
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
		m.loading = true
		m.index = explore.Index{}
		m.status = exploreReadingStatus
		m.statusErr = false
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
	case "esc", "ctrl+c":
		m.filtering = false
		return m, nil
	case "enter":
		m.filtering = false
		m.query.Text = strings.TrimSpace(m.filter)
		_ = m.applyQuery()
		m, cmd := m.schedulePoster()
		return m, cmd
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
	m.clearStatus()
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
		return err
	}
	m.visible = result
	m.query.Status = result.StatusLens
	m.query.Dungeon = result.DungeonLens
	if m.cursor >= len(result.Items) {
		m.cursor = 0
	}
	if len(result.Items) == 0 {
		m.cursor = 0
	}
	return nil
}

func (m exploreModel) schedulePoster() (exploreModel, tea.Cmd) {
	m.loadGen++
	m.tickGen++
	m.cancelDecode()
	m.poster = nil
	m.frames = nil
	m.playing = false
	m.mediaFor = ""
	if m.protocol == explore.ProtocolOff || m.stageRows() == 0 {
		return m, nil
	}
	item, ok := m.focused()
	if !ok || item.Replay == "" {
		return m, nil
	}
	gen := m.loadGen
	return m, tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return exploreDecode{gen: gen}
	})
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
		msg := explorePoster{gen: gen, item: rel, tooLong: errors.Is(err, explore.ErrReplayTooLong)}
		if err != nil && !msg.tooLong {
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
		m.statusErr = true
		m.status = `go needs shell integration: eval "$(camp shell-init <shell>)"`
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
		m.statusErr = true
		m.status = "copy failed: " + err.Error()
		return m, nil
	}
	m.statusErr = false
	m.status = "copied"
	return m, nil
}

func (m *exploreModel) clearStatus() {
	if m.status == "copied" || strings.HasPrefix(m.status, "go needs") || strings.HasPrefix(m.status, "Replay") || strings.HasPrefix(m.status, "The replay") || strings.HasPrefix(m.status, "copy failed") {
		m.status = ""
		m.statusErr = false
	}
}
