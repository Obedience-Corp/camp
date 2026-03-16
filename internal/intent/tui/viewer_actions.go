package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/Obedience-Corp/camp/internal/editor"
	"github.com/Obedience-Corp/camp/internal/intent"
	tea "github.com/charmbracelet/bubbletea"
)

// openInEditor opens the intent in $EDITOR.
func (m IntentViewerModel) openInEditor() tea.Cmd {
	if _, err := os.Stat(m.intent.Path); os.IsNotExist(err) {
		return func() tea.Msg {
			return ViewerEditorFinishedMsg{
				Err:  fmt.Errorf("file no longer exists: %s", filepath.Base(m.intent.Path)),
				Path: m.intent.Path,
			}
		}
	}

	editorName := editor.GetEditor(m.ctx)
	c := editor.BuildEditorCommand(m.ctx, editorName, m.intent.Path)
	// Process group isolation via BuildEditorCommand ensures the editor doesn't inherit parent signals.
	// Terminal editors exit when the controlling terminal closes on parent exit.
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return ViewerEditorFinishedMsg{Err: err, Path: m.intent.Path}
	})
}

// moveIntent moves the intent to a new status.
func (m IntentViewerModel) moveIntent(newStatus intent.Status) tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.Move(m.ctx, m.intent.ID, newStatus)
		return ViewerMoveFinishedMsg{
			Err:       err,
			NewStatus: newStatus,
		}
	}
}

// archiveIntent archives the intent.
func (m IntentViewerModel) archiveIntent() tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.Archive(m.ctx, m.intent.ID)
		return ViewerArchiveFinishedMsg{Err: err}
	}
}

// deleteIntent deletes the intent.
func (m IntentViewerModel) deleteIntent() tea.Cmd {
	return func() tea.Msg {
		err := m.service.Delete(m.ctx, m.intent.ID)
		return ViewerDeleteFinishedMsg{Err: err}
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
