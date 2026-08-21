package explorer

import (
	"fmt"
	"os"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// previewDebounce is how long the cursor must sit still before markdown
// preview is rendered. Rapid j/k bumps previewSeq so earlier ticks are dropped.
const previewDebounce = 50 * time.Millisecond

// previewDebounceMsg fires after the cursor has settled on a row.
type previewDebounceMsg struct {
	seq   uint64
	title string
	path  string
	body  string
	width int
}

// previewLoadedMsg is the result of reading + rendering preview off the
// Update goroutine. Stale seq values are ignored.
type previewLoadedMsg struct {
	seq      uint64
	title    string
	raw      string
	rendered string
}

// schedulePreviewLoad records a new generation and returns a debounce tick.
// Update itself does no file I/O and no glamour work.
func (m *Model) schedulePreviewLoad() tea.Cmd {
	return m.enqueuePreviewLoad(previewDebounce)
}

// schedulePreviewLoadImmediate skips the debounce (explicit "v" toggle).
func (m *Model) schedulePreviewLoadImmediate() tea.Cmd {
	return m.enqueuePreviewLoad(0)
}

func (m *Model) enqueuePreviewLoad(delay time.Duration) tea.Cmd {
	if !m.showPreview {
		return nil
	}
	m.previewSeq++
	selected := m.SelectedIntent()
	if selected == nil {
		return nil
	}
	req := previewDebounceMsg{
		seq:   m.previewSeq,
		title: selected.Title,
		path:  selected.Path,
		body:  selected.Content,
		width: m.previewPane.Width(),
	}
	if delay <= 0 {
		return loadPreviewCmd(req)
	}
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return req
	})
}

func loadPreviewCmd(req previewDebounceMsg) tea.Cmd {
	return func() tea.Msg {
		raw := req.body
		if req.path != "" {
			b, err := os.ReadFile(req.path)
			if err != nil {
				msg := fmt.Sprintf("Could not read file:\n%s", err.Error())
				return previewLoadedMsg{seq: req.seq, title: req.title, raw: "", rendered: msg}
			}
			if len(b) == 0 {
				return previewLoadedMsg{seq: req.seq, title: req.title, raw: "", rendered: "No content"}
			}
			raw = string(b)
		}
		width := req.width
		if width < 20 {
			width = 40
		}
		return previewLoadedMsg{
			seq:      req.seq,
			title:    req.title,
			raw:      raw,
			rendered: tui.RenderPreviewContent(raw, width),
		}
	}
}

func (m Model) handlePreviewDebounce(msg previewDebounceMsg) (tea.Model, tea.Cmd) {
	if m.quitting || msg.seq != m.previewSeq {
		return m, nil
	}
	return m, loadPreviewCmd(msg)
}

func (m Model) handlePreviewLoaded(msg previewLoadedMsg) (tea.Model, tea.Cmd) {
	if m.quitting || msg.seq != m.previewSeq {
		return m, nil
	}
	m.previewPane.ApplyRendered(msg.title, msg.raw, msg.rendered)
	return m, nil
}
