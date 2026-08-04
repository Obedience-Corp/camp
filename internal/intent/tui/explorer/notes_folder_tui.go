package explorer

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
)

// folderFinishedMsg signals a note-folder mutation finished.
type folderFinishedMsg struct {
	err     error
	message string
}

// selectedNoteFolder returns the IntentGroup under the cursor when it is a
// note-folder header (cursor on header, status is a note path).
func (m *Model) selectedNoteFolder() *IntentGroup {
	if len(m.groups) == 0 || m.cursorItem != -1 {
		return nil
	}
	g := &m.groups[m.cursorGroup]
	if g.Status.IsNote() || g.Status == intent.StatusNote {
		return g
	}
	return nil
}

// startFolderCreate opens a text input to create a note folder. Parent path is
// inferred from the cursor: under a note folder header, create as a child;
// otherwise create under the notes root.
func (m *Model) startFolderCreate() {
	if m.service == nil {
		m.statusMessage = "Folder create requires an intent service"
		return
	}
	parent := ""
	if g := m.selectedNoteFolder(); g != nil {
		if g.Status != intent.StatusNote {
			parent = strings.TrimPrefix(string(g.Status), "notes/")
		}
	}
	m.folderParentRel = parent
	ti := textinput.New()
	ti.Placeholder = "folder-name or nested/path"
	ti.CharLimit = 80
	ti.Width = 50
	ti.Focus()
	m.folderInput = ti
	m.folderAction = "create"
	m.focus = focusFolderInput
}

// startFolderRename opens rename input for the note folder under the cursor.
func (m *Model) startFolderRename() {
	g := m.selectedNoteFolder()
	if g == nil {
		return
	}
	if g.Status == intent.StatusNote {
		m.statusMessage = "Cannot rename the notes root"
		return
	}
	if g.Status == intent.StatusNoteArchived || g.Status == intent.StatusNoteMeetings {
		m.statusMessage = "Cannot rename reserved note folders"
		return
	}
	if m.service == nil {
		m.statusMessage = "Folder rename requires an intent service"
		return
	}
	rel := strings.TrimPrefix(string(g.Status), "notes/")
	m.folderParentRel = rel // source path for rename
	ti := textinput.New()
	ti.CharLimit = 80
	ti.Width = 50
	ti.SetValue(filepath.Base(rel))
	ti.CursorEnd()
	ti.Focus()
	m.folderInput = ti
	m.folderAction = "rename"
	m.focus = focusFolderInput
}

// deleteSelectedFolder removes an empty user note folder under the cursor.
func (m *Model) deleteSelectedFolder() tea.Cmd {
	g := m.selectedNoteFolder()
	if g == nil {
		return nil
	}
	if g.Status == intent.StatusNote {
		m.statusMessage = "Cannot delete the notes root"
		return nil
	}
	if g.Status == intent.StatusNoteArchived || g.Status == intent.StatusNoteMeetings {
		m.statusMessage = "Cannot delete reserved note folders"
		return nil
	}
	if m.service == nil {
		m.statusMessage = "Folder delete requires an intent service"
		return nil
	}
	rel := strings.TrimPrefix(string(g.Status), "notes/")
	return m.runDeleteFolder(rel)
}

// startNoteFolderMove opens a folder picker for moving the selected note.
func (m *Model) startNoteFolderMove(note *intent.Intent) {
	if note == nil || !note.Status.IsNote() {
		return
	}
	if m.service == nil {
		m.statusMessage = "Note move requires an intent service"
		return
	}
	folders, err := m.service.NoteFolders(m.ctx)
	if err != nil {
		m.statusMessage = "Failed to list note folders: " + err.Error()
		return
	}
	// Picker options: root first, then non-archived folders.
	opts := make([]noteFolderOption, 0, len(folders))
	for _, f := range folders {
		if f.Status == intent.StatusNoteArchived ||
			strings.HasPrefix(string(f.Status), string(intent.StatusNoteArchived)+"/") {
			continue
		}
		rel := ""
		if f.Status != intent.StatusNote {
			rel = strings.TrimPrefix(string(f.Status), "notes/")
		}
		label := f.Name
		if rel != "" {
			label = strings.Repeat("  ", f.Depth) + f.Name
		}
		opts = append(opts, noteFolderOption{label: label, rel: rel, status: f.Status})
	}
	m.noteFolderOptions = opts
	m.noteFolderIdx = 0
	m.noteToMoveFolder = note
	m.focus = focusNoteFolderMove
}

type noteFolderOption struct {
	label  string
	rel    string
	status intent.Status
}

func (m *Model) updateFolderInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.focus = focusList
		m.folderAction = ""
		m.folderParentRel = ""
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.folderInput.Value())
		action := m.folderAction
		parent := m.folderParentRel
		m.focus = focusList
		m.folderAction = ""
		m.folderParentRel = ""
		if value == "" {
			return m, nil
		}
		switch action {
		case "create":
			rel := value
			if parent != "" {
				rel = parent + "/" + value
			}
			return m, m.runCreateFolder(rel)
		case "rename":
			// parent holds the full from-rel; value is the new base name (or full rel).
			from := parent
			to := value
			if !strings.Contains(value, "/") {
				// Replace last segment of from with value.
				dir := filepath.Dir(from)
				if dir == "." {
					to = value
				} else {
					to = dir + "/" + value
				}
			}
			return m, m.runRenameFolder(from, to)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.folderInput, cmd = m.folderInput.Update(msg)
	return m, cmd
}

func (m *Model) updateNoteFolderMove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.focus = focusList
		m.noteToMoveFolder = nil
		m.noteFolderOptions = nil
		return m, nil
	case "j", "down":
		if m.noteFolderIdx < len(m.noteFolderOptions)-1 {
			m.noteFolderIdx++
		}
	case "k", "up":
		if m.noteFolderIdx > 0 {
			m.noteFolderIdx--
		}
	case "enter":
		if m.noteToMoveFolder == nil || len(m.noteFolderOptions) == 0 {
			m.focus = focusList
			return m, nil
		}
		opt := m.noteFolderOptions[m.noteFolderIdx]
		note := m.noteToMoveFolder
		m.focus = focusList
		m.noteToMoveFolder = nil
		m.noteFolderOptions = nil
		return m, m.runMoveNoteToFolder(note, opt.rel)
	}
	return m, nil
}

func (m *Model) runCreateFolder(rel string) tea.Cmd {
	return func() tea.Msg {
		folder, err := m.service.CreateNoteFolder(m.ctx, rel)
		if err != nil {
			return folderFinishedMsg{err: err}
		}
		_ = m.appendAuditEvent(audit.Event{
			Type:   audit.EventCreate,
			ID:     string(folder.Status),
			Title:  folder.Name,
			To:     string(folder.Status),
			Reason: "created note folder",
		})
		m.autoCommitIntent(commit.IntentCreate, folder.Name, "Created note folder "+string(folder.Status))
		return folderFinishedMsg{message: "Created folder " + string(folder.Status)}
	}
}

func (m *Model) runRenameFolder(from, to string) tea.Cmd {
	return func() tea.Msg {
		if err := m.service.RenameNoteFolder(m.ctx, from, to); err != nil {
			return folderFinishedMsg{err: err}
		}
		_ = m.appendAuditEvent(audit.Event{
			Type:   audit.EventRename,
			ID:     "notes/" + from,
			Title:  filepath.Base(to),
			From:   "notes/" + from,
			To:     "notes/" + to,
			Reason: "renamed note folder",
		})
		m.autoCommitIntent(commit.IntentMove, filepath.Base(to), "Renamed note folder "+from+" → "+to)
		return folderFinishedMsg{message: "Renamed folder " + from + " → " + to}
	}
}

func (m *Model) runDeleteFolder(rel string) tea.Cmd {
	return func() tea.Msg {
		if err := m.service.DeleteNoteFolder(m.ctx, rel); err != nil {
			return folderFinishedMsg{err: err}
		}
		_ = m.appendAuditEvent(audit.Event{
			Type:   audit.EventDelete,
			ID:     "notes/" + rel,
			Title:  filepath.Base(rel),
			From:   "notes/" + rel,
			Reason: "deleted empty note folder",
		})
		m.autoCommitIntent(commit.IntentMove, filepath.Base(rel), "Deleted note folder "+rel)
		return folderFinishedMsg{message: "Deleted folder " + rel}
	}
}

func (m *Model) runMoveNoteToFolder(note *intent.Intent, folderRel string) tea.Cmd {
	return func() tea.Msg {
		oldPath := note.Path
		from := string(note.Status)
		moved, err := m.service.MoveNoteToFolder(m.ctx, note.ID, folderRel)
		if err != nil {
			return folderFinishedMsg{err: err}
		}
		_ = m.appendAuditEvent(audit.Event{
			Type:  audit.EventMove,
			ID:    moved.ID,
			Title: moved.Title,
			From:  from,
			To:    string(moved.Status),
		})
		m.autoCommitIntent(commit.IntentMove, moved.Title, "Moved note to "+string(moved.Status), oldPath, moved.Path)
		return folderFinishedMsg{message: "Moved note to " + string(moved.Status)}
	}
}

func (m Model) viewFolderInput() string {
	var b strings.Builder
	title := "Create Note Folder"
	if m.folderAction == "rename" {
		title = "Rename Note Folder"
	}
	b.WriteString(tui.TitleStyle.Render(title))
	b.WriteString("\n\n")
	if m.folderParentRel != "" && m.folderAction == "create" {
		b.WriteString(tui.HelpStyle.Render("Parent: notes/"+m.folderParentRel) + "\n\n")
	}
	if m.folderAction == "rename" {
		b.WriteString(tui.HelpStyle.Render("From: notes/"+m.folderParentRel) + "\n\n")
	}
	b.WriteString(m.folderInput.View())
	b.WriteString("\n\n")
	b.WriteString(tui.HelpStyle.Render("Enter: confirm . Esc: cancel"))
	return b.String()
}

func (m Model) viewNoteFolderMove() string {
	var b strings.Builder
	b.WriteString(tui.TitleStyle.Render("Move Note to Folder"))
	b.WriteString("\n\n")
	if m.noteToMoveFolder != nil {
		b.WriteString("Note: " + m.noteToMoveFolder.Title + "\n\n")
	}
	for i, opt := range m.noteFolderOptions {
		cursor := "  "
		if i == m.noteFolderIdx {
			cursor = "> "
		}
		b.WriteString(cursor + opt.label + "\n")
	}
	b.WriteString("\n")
	b.WriteString(tui.HelpStyle.Render("j/k: navigate . Enter: move . Esc: cancel"))
	return b.String()
}
