package tui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case refreshMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.allItems = msg.items
			m.refilter()
		}
		return m, nil

	case editorFinishedMsg:
		// Editor exited, nothing to do
		return m, nil

	case tea.KeyMsg:
		if m.searchMode {
			return m.handleSearchKey(msg)
		}
		return m.handleNormalKey(msg)
	}
	return m, nil
}

func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Handle gg (two-key sequence)
	if key == "g" {
		if m.lastKeyWasG {
			m.cursor = 0
			m.lastKeyWasG = false
			return m, nil
		}
		m.lastKeyWasG = true
		return m, nil
	}
	m.lastKeyWasG = false

	switch key {
	// Navigation
	case "j", "down":
		if m.cursor < len(m.filteredItems)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "G":
		if len(m.filteredItems) > 0 {
			m.cursor = len(m.filteredItems) - 1
		}

	// Search
	case "/":
		m.searchMode = true
		m.searchInput.Focus()
		return m, nil

	// Type filters
	case "0":
		m.typeFilter = ""
		m.refilter()
	case "1":
		m.typeFilter = "intent"
		m.refilter()
	case "2":
		m.typeFilter = "design"
		m.refilter()
	case "3":
		m.typeFilter = "explore"
		m.refilter()
	case "4":
		m.typeFilter = "festival"
		m.refilter()

	// Preview toggle
	case "tab", "p":
		if isWideLayout(m.width) {
			m.showPreview = !m.showPreview
		} else {
			m.previewOverlay = !m.previewOverlay
		}

	// Help
	case "?":
		m.helpVisible = !m.helpVisible

	// Refresh
	case "r":
		return m, m.refreshCmd()

	// Selection
	case "enter":
		if len(m.filteredItems) > 0 {
			item := m.filteredItems[m.cursor]
			m.Selected = &item
			return m, tea.Quit
		}

	// Quick actions (read-only)
	case "e":
		if item := m.currentItem(); item.PrimaryDoc != "" {
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			c := exec.Command(editor, item.PrimaryDoc)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return editorFinishedMsg{err: err}
			})
		}
	case "o":
		if item := m.currentItem(); item.AbsolutePath != "" {
			var c *exec.Cmd
			switch runtime.GOOS {
			case "darwin":
				c = exec.Command("open", item.AbsolutePath)
			default:
				c = exec.Command("xdg-open", item.AbsolutePath)
			}
			_ = c.Start()
		}
	case "y":
		if item := m.currentItem(); item.AbsolutePath != "" {
			var c *exec.Cmd
			switch runtime.GOOS {
			case "darwin":
				c = exec.Command("pbcopy")
			default:
				c = exec.Command("xclip", "-selection", "clipboard")
			}
			c.Stdin = strings.NewReader(item.AbsolutePath)
			_ = c.Run()
		}

	// Quit
	case "q", "ctrl+c":
		return m, tea.Quit

	// Escape clears overlays
	case "esc":
		if m.helpVisible {
			m.helpVisible = false
		} else if m.previewOverlay {
			m.previewOverlay = false
		} else if m.searchQuery != "" {
			m.searchQuery = ""
			m.searchInput.SetValue("")
			m.refilter()
		}
	}
	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.searchMode = false
		m.searchInput.Blur()
		if msg.String() == "esc" {
			m.searchInput.SetValue(m.searchQuery) // restore previous
		} else {
			m.searchQuery = m.searchInput.Value()
			m.refilter()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	// Live filter as user types
	m.searchQuery = m.searchInput.Value()
	m.refilter()
	return m, cmd
}
