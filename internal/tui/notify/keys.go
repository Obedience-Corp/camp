package notify

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/notice"
)

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	if m.busy {
		switch k {
		case "q", "esc", "ctrl+c":
			return m.quitWhenSettled(k)
		}
		return m, nil
	}
	if k == "ctrl+c" {
		return m.quit()
	}
	if m.showHelp {
		switch k {
		case "?", "esc", "q", "enter":
			m.showHelp = false
		}
		return m, nil
	}

	m.status = ""
	switch k {
	case "q", "esc":
		return m.quit()
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(len(m.rows())-1, 0)
	case "enter":
		m.showDetail = !m.showDetail
	case "d":
		return m.dismissSelected()
	case "r":
		return m.restoreSelected()
	case "y", "c":
		return m.copySelected(), nil
	case "?":
		m.showHelp = true
	}
	return m, nil
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

// quitWhenSettled defers a quit until the write in flight reports, so its
// outcome is recorded before the exit report is printed. A second ctrl+c
// quits at once for a write that never returns; Unsettled names it then.
func (m Model) quitWhenSettled(key string) (tea.Model, tea.Cmd) {
	if m.quitPending && key == "ctrl+c" {
		return m.quit()
	}
	m.quitPending = true
	m.setStatus("finishing, then quitting… (ctrl+c again quits now)", false)
	return m, nil
}

func (m *Model) move(delta int) {
	n := len(m.rows())
	if n == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), n-1)
}

func (m Model) dismissSelected() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		m.setStatus("no notices to dismiss", true)
		return m, nil
	}
	if r.section == sectionDismissed {
		m.setStatus(r.id+" is already dismissed; r restores it", true)
		return m, nil
	}
	m.busy = true
	m.inFlight = &Change{ID: r.id, Dismissed: true}
	m.setStatus("dismissing…", false)
	return m, m.write(r, true)
}

func (m Model) restoreSelected() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		m.setStatus("no notices to restore", true)
		return m, nil
	}
	if r.section == sectionLive {
		m.setStatus(r.id+" is not dismissed; d dismisses it", true)
		return m, nil
	}
	m.busy = true
	m.inFlight = &Change{ID: r.id, Dismissed: false}
	m.setStatus("restoring…", false)
	return m, m.write(r, false)
}

// write records the change, then re-runs every detector rather than moving
// the row by hand: an artifact detector reports only its first undismissed
// root, so dismissing one can surface the next.
func (m Model) write(target row, dismiss bool) tea.Cmd {
	ctx, store := m.ctx, m.store
	return func() tea.Msg {
		msg := actionMsg{target: target, dismiss: dismiss}
		if dismiss {
			msg.writeErr = store.Dismiss(ctx, target.id)
		} else {
			msg.writeErr = store.Restore(ctx, target.id)
		}
		if msg.writeErr != nil {
			return msg
		}
		msg.inv, msg.reloadErr = store.Inventory(ctx)
		return msg
	}
}

func (m Model) applyAction(msg actionMsg) Model {
	m.busy = false
	m.inFlight = nil
	verb, done := "restore", "restored"
	if msg.dismiss {
		verb, done = "dismiss", "dismissed"
	}
	if msg.writeErr != nil {
		m.setStatus(verb+" failed: "+msg.writeErr.Error(), true)
		return m
	}
	m.recordChange(msg.target.id, msg.dismiss)
	if msg.reloadErr != nil {
		m.setStatus(done+" "+msg.target.id+", but reloading notices failed: "+msg.reloadErr.Error(), true)
		return m
	}

	idx := m.sectionIndex(msg.target)
	m.inv = msg.inv
	m.place(msg.target.section, idx)
	switch {
	case msg.dismiss:
		m.setStatus("dismissed "+named(msg.target)+" · undo: r on it under Dismissed", false)
	case m.isLive(msg.target.id):
		m.setStatus("restored "+named(msg.target), false)
	default:
		m.setStatus("restored "+named(msg.target)+"; it is not detected right now", false)
	}
	return m
}

func (m Model) copySelected() Model {
	r, ok := m.selected()
	if !ok {
		m.setStatus("nothing to copy", true)
		return m
	}
	if r.command == "" {
		m.setStatus("no fix on record for "+r.id+"; camp keeps only the id of a dismissal", true)
		return m
	}
	if m.clipboard == nil {
		m.setStatus("copy failed: no clipboard available", true)
		return m
	}
	runnable, elsewhere := notice.RunnableCommand(r.command)
	if err := m.clipboard(runnable); err != nil {
		m.setStatus("copy failed: "+err.Error(), true)
		return m
	}
	m.setStatus(copiedStatus(runnable, elsewhere), false)
	return m
}

func copiedStatus(command string, elsewhere bool) string {
	placeholder := notice.HasPlaceholder(command)
	switch {
	case elsewhere && placeholder:
		return "copied " + command + " · fill in the <…> value and run it on another machine"
	case elsewhere:
		return "copied " + command + " · run it on another machine"
	case placeholder:
		return "copied " + command + " · fill in the <…> value before running it"
	default:
		return "copied " + command
	}
}

// named is how a status line refers to a notice: its id, plus its subject when
// the id alone is a hash.
func named(r row) string {
	if r.subject == "" {
		return r.id
	}
	return r.id + " (" + r.subject + ")"
}
