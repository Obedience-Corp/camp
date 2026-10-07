// Package notify is the camp notify browser: live notices and dismissed ones,
// with dismiss, restore, and copy-the-fix in place, so nobody has to carry a
// notice id from a status line to a command by hand.
package notify

import (
	"context"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/notice"
)

type section int

const (
	sectionLive section = iota
	sectionDismissed
)

type row struct {
	section section
	id      string
	subject string
	summary string
	message string
	command string
	at      time.Time
}

// label is how a row reads in the list: the notice's own message while a
// detector reports it, otherwise what the id says it was about.
func (r row) label() string {
	switch {
	case r.message != "":
		return r.message
	case r.subject != "" && r.summary != "":
		return r.subject + ": " + r.summary
	case r.summary != "":
		return r.summary
	default:
		return r.id
	}
}

// Change is one dismissal the session left changed, net of undoing it.
type Change struct {
	ID        string
	Dismissed bool
}

// Options wires the browser to its dependencies.
type Options struct {
	Store     Store
	Clipboard func(string) error
}

// Model is the Bubble Tea model for camp notify.
type Model struct {
	ctx        context.Context
	store      Store
	clipboard  func(string) error
	inv        notice.Inventory
	cursor     int
	showDetail bool
	showHelp   bool
	busy       bool
	status     string
	statusErr  bool
	width      int
	height     int
	quitting   bool
	changed    map[string]bool
	// inFlight is the write a pending actionMsg will report. quitPending
	// holds a quit until it lands, so the exit report never omits a change
	// that reached the disk.
	inFlight    *Change
	quitPending bool
}

// New returns a browser over inv. The detail pane starts open because the
// fix command is the part of a notice a person came here to act on.
func New(ctx context.Context, inv notice.Inventory, opts Options) Model {
	return Model{
		ctx:        ctx,
		store:      opts.Store,
		clipboard:  opts.Clipboard,
		inv:        inv,
		showDetail: true,
		changed:    map[string]bool{},
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Changes reports the dismissals this session added or removed, by id.
func (m Model) Changes() []Change {
	out := make([]Change, 0, len(m.changed))
	for id, dismissed := range m.changed {
		out = append(out, Change{ID: id, Dismissed: dismissed})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Unsettled reports a write still in flight when the browser exited, which
// only happens when a second ctrl+c forced the quit. Whether it reached the
// disk is unknown.
func (m Model) Unsettled() []Change {
	if m.inFlight == nil {
		return nil
	}
	return []Change{*m.inFlight}
}

type actionMsg struct {
	target    row
	dismiss   bool
	inv       notice.Inventory
	writeErr  error
	reloadErr error
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case actionMsg:
		m = m.applyAction(msg)
		if m.quitPending {
			return m.quit()
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) rows() []row {
	out := make([]row, 0, len(m.inv.Live)+len(m.inv.Dismissed))
	for _, n := range m.inv.Live {
		out = append(out, row{section: sectionLive, id: n.ID, subject: n.Subject, message: n.Message, command: n.Command})
	}
	for _, d := range m.inv.Dismissed {
		r := row{section: sectionDismissed, id: d.ID, at: d.At, subject: d.Subject, summary: d.Summary}
		if r.subject == "" && notice.HasSubject(d.ID) {
			r.subject = "root no longer declared"
		}
		if d.Notice != nil {
			r.message, r.command = d.Notice.Message, d.Notice.Command
		}
		out = append(out, r)
	}
	return out
}

func (m Model) selected() (row, bool) {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return row{}, false
	}
	return rows[m.cursor], true
}

// sectionIndex is the selected row's position inside its own section.
func (m Model) sectionIndex(r row) int {
	if r.section == sectionDismissed {
		return m.cursor - len(m.inv.Live)
	}
	return m.cursor
}

// place keeps the cursor in the section an action started from, so a run of
// dismissals walks down the live list instead of chasing each notice into
// the dismissed one.
func (m *Model) place(sec section, idx int) {
	live, dismissed := len(m.inv.Live), len(m.inv.Dismissed)
	switch {
	case sec == sectionLive && live > 0:
		m.cursor = min(idx, live-1)
	case sec == sectionDismissed && dismissed > 0:
		m.cursor = live + min(idx, dismissed-1)
	case sec == sectionLive:
		m.cursor = 0
	default:
		m.cursor = max(live-1, 0)
	}
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.statusErr = s, isErr
}

func (m Model) isLive(id string) bool {
	for _, n := range m.inv.Live {
		if n.ID == id {
			return true
		}
	}
	return false
}

func (m *Model) recordChange(id string, dismissed bool) {
	if prev, ok := m.changed[id]; ok && prev != dismissed {
		delete(m.changed, id)
		return
	}
	m.changed[id] = dismissed
}
