package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/shell"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type artifactLoadedMsg struct {
	files []artifacts.InventoryFile
	err   error
}
type artifactActionMsg struct {
	action, path string
	err          error
	wait         tea.Cmd
}
type artifactOpenExitMsg struct{ err error }
type artifactModel struct {
	ctx                                             context.Context
	root                                            string
	all, visible                                    []artifacts.InventoryFile
	cursor, width, height                           int
	input                                           textinput.Model
	filtering, loading, busy, gotoEnabled, quitting bool
	status                                          string
	statusErr                                       bool
	gotoPath                                        string
}

func newArtifactModel(ctx context.Context, root string, gotoEnabled bool) artifactModel {
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "Filter by name or folder"
	input.CharLimit = 256
	return artifactModel{ctx: ctx, root: root, input: input, loading: true, gotoEnabled: gotoEnabled}
}
func (m artifactModel) load() tea.Msg {
	files, err := artifacts.Inventory(m.ctx, m.root)
	return artifactLoadedMsg{files, err}
}
func (m artifactModel) Init() tea.Cmd { return m.load }
func (m *artifactModel) filter() {
	selected := ""
	if m.cursor < len(m.visible) {
		selected = m.visible[m.cursor].Path
	}
	m.visible = nil
	query := strings.ToLower(strings.TrimSpace(m.input.Value()))
	for _, file := range m.all {
		if strings.Contains(strings.ToLower(file.Path), query) {
			m.visible = append(m.visible, file)
		}
	}
	m.cursor = ui.ClampIdx(m.cursor, len(m.visible))
	for i, file := range m.visible {
		if file.Path == selected {
			m.cursor = i
			break
		}
	}
}
func (m artifactModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(1, msg.Width-6)
	case artifactLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.statusErr = true
		} else {
			m.all = msg.files
			m.filter()
			m.status = ""
			m.statusErr = false
		}
	case artifactActionMsg:
		m.busy = false
		m.statusErr = msg.err != nil
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		switch msg.action {
		case "go":
			m.gotoPath = msg.path
			m.quitting = true
			return m, tea.Quit
		case "copy":
			m.status = "Copied path"
		case "open":
			m.status = "Opened " + artifactDisplay(filepath.Base(msg.path))
			return m, msg.wait
		}
	case artifactOpenExitMsg:
		m.status = msg.err.Error()
		m.statusErr = true
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.input.SetValue("")
				m.filtering = false
				m.input.Blur()
				m.filter()
				return m, nil
			case "enter":
				m.filtering = false
				m.input.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.filter()
			return m, cmd
		}
		switch msg.String() {
		case "q", "esc":
			m.quitting = true
			return m, tea.Quit
		case "/":
			m.filtering = true
			return m, m.input.Focus()
		case "up", "k":
			m.cursor = ui.ClampIdx(m.cursor-1, len(m.visible))
		case "down", "j":
			m.cursor = ui.ClampIdx(m.cursor+1, len(m.visible))
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = ui.ClampIdx(len(m.visible)-1, len(m.visible))
		case "pgup":
			m.cursor = ui.ClampIdx(m.cursor-max(1, m.height-12), len(m.visible))
		case "pgdown":
			m.cursor = ui.ClampIdx(m.cursor+max(1, m.height-12), len(m.visible))
		case "r":
			if !m.loading {
				m.loading = true
				return m, m.load
			}
		case "enter", "o", "g", "y":
			if len(m.visible) == 0 || m.busy {
				return m, nil
			}
			action := "open"
			if msg.String() == "g" {
				if !m.gotoEnabled {
					m.status = "Go needs shell integration: " + shell.InitHint()
					m.statusErr = false
					return m, nil
				}
				action = "go"
			}
			if msg.String() == "y" {
				action = "copy"
			}
			m.busy = true
			m.status = "Working…"
			m.statusErr = false
			return m, artifactAction(m.ctx, m.root, m.visible[m.cursor].Path, action)
		}
	default:
		if m.filtering {
			before := m.input.Value()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			if m.input.Value() != before {
				m.filter()
			}
			return m, cmd
		}
	}
	return m, nil
}
