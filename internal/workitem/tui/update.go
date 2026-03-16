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
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
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
			m.clampScroll()
			return m, nil
		}
		m.lastKeyWasG = true
		return m, nil
	}
	m.lastKeyWasG = false

	// Type filter keys (0-4) — handled via lookup table
	if filter, ok := typeFilterKeys[key]; ok {
		m.typeFilter = filter
		m.refilter()
		return m, nil
	}

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
		m.clampScroll()
		return m, nil

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
		return m.openEditor()
	case "o":
		return m.openSystemHandler()
	case "y":
		return m.copyPath()

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
	m.clampScroll()
	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.searchMode = false
		m.searchInput.Blur()
		if msg.String() == "esc" {
			m.searchInput.SetValue(m.searchQuery)
		} else {
			m.searchQuery = m.searchInput.Value()
			m.refilter()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.searchQuery = m.searchInput.Value()
	m.refilter()
	return m, cmd
}

// --- Quick action implementations ---
// Extracted from the switch to keep key handling readable and action
// logic (platform-specific exec, env vars) in focused methods.

func (m Model) openEditor() (tea.Model, tea.Cmd) {
	item := m.currentItem()
	if item.PrimaryDoc == "" {
		return m, nil
	}
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

func (m Model) openSystemHandler() (tea.Model, tea.Cmd) {
	item := m.currentItem()
	if item.AbsolutePath == "" {
		return m, nil
	}
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", item.AbsolutePath)
	default:
		c = exec.Command("xdg-open", item.AbsolutePath)
	}
	if err := c.Start(); err != nil {
		cmd := m.setStatus("open failed: " + err.Error())
		return m, cmd
	}
	cmd := m.setStatus("opened")
	return m, cmd
}

func (m Model) copyPath() (tea.Model, tea.Cmd) {
	item := m.currentItem()
	if item.AbsolutePath == "" {
		return m, nil
	}
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("pbcopy")
	default:
		c = exec.Command("xclip", "-selection", "clipboard")
	}
	c.Stdin = strings.NewReader(item.AbsolutePath)
	if err := c.Run(); err != nil {
		cmd := m.setStatus("copy failed: " + err.Error())
		return m, cmd
	}
	cmd := m.setStatus("copied!")
	return m, cmd
}

