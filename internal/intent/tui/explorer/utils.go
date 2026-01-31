package explorer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// formatRelativeTime returns a human-friendly relative time string.
func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("Jan 2")
	}
}

// openInEditor opens a file in the user's $EDITOR.
func openInEditor(filePath string) tea.Cmd {
	// Check file exists before opening
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return func() tea.Msg {
			return editorFinishedMsg{
				err:  fmt.Errorf("file no longer exists: %s", filepath.Base(filePath)),
				path: filePath,
			}
		}
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	c := exec.Command(editor, filePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err, path: filePath}
	})
}

// openWithSystem opens a file with the system's default handler.
func openWithSystem(filePath string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", filePath)
		case "linux":
			cmd = exec.Command("xdg-open", filePath)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", filePath)
		default:
			return openFinishedMsg{err: fmt.Errorf("unsupported platform: %s", runtime.GOOS)}
		}
		err := cmd.Start()
		return openFinishedMsg{err: err}
	}
}

// revealInFileManager opens the file manager and selects the file.
func revealInFileManager(filePath string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			// macOS: open -R reveals in Finder and selects the file
			cmd = exec.Command("open", "-R", filePath)
		case "linux":
			// Linux: open the containing directory
			cmd = exec.Command("xdg-open", filepath.Dir(filePath))
		case "windows":
			// Windows: explorer /select, highlights the file
			cmd = exec.Command("explorer", "/select,", filePath)
		default:
			return openFinishedMsg{err: fmt.Errorf("unsupported platform: %s", runtime.GOOS)}
		}
		err := cmd.Start()
		return openFinishedMsg{err: err}
	}
}
