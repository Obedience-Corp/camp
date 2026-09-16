package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/editor"
	"github.com/Obedience-Corp/camp/internal/intent"
	tea "github.com/charmbracelet/bubbletea"
)

// openInEditor opens the intent in $EDITOR.
func (m IntentViewerModel) openInEditor() tea.Cmd {
	if _, err := os.Stat(m.intent.Path); os.IsNotExist(err) {
		return func() tea.Msg {
			return ViewerEditorFinishedMsg{
				Err:  camperrors.Newf("file no longer exists: %s", filepath.Base(m.intent.Path)),
				Path: m.intent.Path,
			}
		}
	}

	editorName := editor.GetEditor(m.ctx)
	c := editor.BuildEditorCommand(m.ctx, editorName, m.intent.Path)
	// Use tea.ExecProcess so Bubble Tea can hand terminal control to the editor
	// and restore the viewer afterward.
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return ViewerEditorFinishedMsg{Err: err, Path: m.intent.Path}
	})
}

// moveIntent moves the intent to a new status.
func (m IntentViewerModel) moveIntent(newStatus intent.Status) tea.Cmd {
	return func() tea.Msg {
		fromPath := m.intent.Path
		from := m.intent.Status
		moved, err := m.service.Move(m.ctx, m.intent.ID, newStatus)
		toPath := ""
		if err == nil && moved != nil {
			toPath = moved.Path
		}
		return ViewerMoveFinishedMsg{
			Err:       err,
			ID:        m.intent.ID,
			Title:     m.intent.Title,
			FromPath:  fromPath,
			ToPath:    toPath,
			From:      from,
			NewStatus: newStatus,
		}
	}
}

// archiveIntent archives the intent.
func (m IntentViewerModel) archiveIntent() tea.Cmd {
	return func() tea.Msg {
		fromPath := m.intent.Path
		from := m.intent.Status
		archived, err := m.service.Archive(m.ctx, m.intent.ID)
		toPath := ""
		if err == nil && archived != nil {
			toPath = archived.Path
		}
		return ViewerArchiveFinishedMsg{
			Err:      err,
			ID:       m.intent.ID,
			Title:    m.intent.Title,
			FromPath: fromPath,
			ToPath:   toPath,
			From:     from,
		}
	}
}

// deleteIntent deletes the intent.
func (m IntentViewerModel) deleteIntent() tea.Cmd {
	return func() tea.Msg {
		err := m.service.Delete(m.ctx, m.intent.ID)
		return ViewerDeleteFinishedMsg{
			Err:    err,
			ID:     m.intent.ID,
			Title:  m.intent.Title,
			Path:   m.intent.Path,
			Status: m.intent.Status,
		}
	}
}

// openWithSystem opens the intent with the system handler.
func (m IntentViewerModel) openWithSystem() tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", m.intent.Path)
		case "linux":
			cmd = exec.Command("xdg-open", m.intent.Path)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", m.intent.Path)
		default:
			return nil
		}
		_ = cmd.Start() // Intentionally ignoring error for background process
		return nil
	}
}

// revealInFileManager reveals the file in the file manager.
func (m IntentViewerModel) revealInFileManager() tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", "-R", m.intent.Path)
		case "linux":
			cmd = exec.Command("xdg-open", filepath.Dir(m.intent.Path))
		case "windows":
			cmd = exec.Command("explorer", "/select,", m.intent.Path)
		default:
			return nil
		}
		_ = cmd.Start() // Intentionally ignoring error for background process
		return nil
	}
}
