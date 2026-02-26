// Package status provides a BubbleTea TUI for viewing git status across all repos.
package status

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var pal = theme.TUI()

// RepoInfo holds summary and detail status for a repository.
type RepoInfo struct {
	Name      string
	Path      string
	Branch    string
	Staged    int
	Modified  int
	Untracked int
	Ahead     int
	Behind    int
	Clean     bool
	Error     string
}

// Model is the BubbleTea model for the status viewer.
type Model struct {
	repos    []RepoInfo
	cursor   int
	viewport viewport.Model
	width    int
	height   int
	ready    bool

	// Cached full git status for each repo
	statusCache map[int]string

	// Vim navigation state
	lastKeyWasG bool

	// Search mode
	searchMode  bool
	searchInput textinput.Model
	searchQuery string
}

// New creates a new status TUI model with the given repo data.
func New(repos []RepoInfo) Model {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 64

	return Model{
		repos:       repos,
		statusCache: make(map[int]string),
		searchInput: ti,
	}
}

// statusMsg carries the result of loading full git status for a repo.
type statusMsg struct {
	index  int
	output string
}

// Init returns the initial command for the model.
func (m Model) Init() tea.Cmd {
	if len(m.repos) == 0 {
		return nil
	}
	return m.loadStatus(0)
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle search mode input
		if m.searchMode {
			return m.updateSearch(msg)
		}

		prev := m.cursor
		key := msg.String()

		// Reset gg tracking on non-g key
		if key != "g" && m.lastKeyWasG {
			m.lastKeyWasG = false
		}

		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.searchQuery != "" {
				m.searchQuery = ""
				m.viewport.SetContent(m.currentStatusContent())
				return m, nil
			}
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.repos)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "right", "tab":
			m.cursor = (m.cursor + 1) % len(m.repos)
		case "left", "shift+tab":
			m.cursor = (m.cursor - 1 + len(m.repos)) % len(m.repos)
		case "g":
			if m.lastKeyWasG {
				m.cursor = 0
				m.lastKeyWasG = false
			} else {
				m.lastKeyWasG = true
				return m, nil
			}
		case "G":
			m.cursor = len(m.repos) - 1
		case "/":
			m.searchMode = true
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			return m, textinput.Blink
		}

		if m.cursor != prev {
			m.viewport.GotoTop()
			m.viewport.SetContent(m.currentStatusContent())
			return m, m.ensureStatus(m.cursor)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpWidth := m.rightPaneWidth()
		vpHeight := max(m.height-4, 1) // header + footer
		m.viewport = viewport.New(vpWidth, vpHeight)
		m.viewport.SetContent(m.currentStatusContent())
		m.ready = true

	case statusMsg:
		m.statusCache[msg.index] = msg.output
		if msg.index == m.cursor {
			m.viewport.SetContent(m.currentStatusContent())
		}
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

// updateSearch handles key input while in search mode.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchQuery = ""
		m.viewport.SetContent(m.currentStatusContent())
		return m, nil
	case "enter":
		m.searchMode = false
		m.searchQuery = m.searchInput.Value()
		m.viewport.SetContent(m.currentStatusContent())
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	// Live preview search highlighting
	m.searchQuery = m.searchInput.Value()
	m.viewport.SetContent(m.currentStatusContent())
	return m, cmd
}

// View renders the TUI.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	leftWidth := m.leftPaneWidth()
	rightWidth := m.rightPaneWidth()

	// Styles
	leftBorder := lipgloss.NewStyle().
		Width(leftWidth).
		Height(m.height - 2).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(pal.Border)

	rightPane := lipgloss.NewStyle().
		Width(rightWidth).
		Height(m.height - 2)

	// Build left pane content
	var left strings.Builder
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	left.WriteString(headerStyle.Render("  Repositories"))
	left.WriteString("\n")
	left.WriteString(lipgloss.NewStyle().Foreground(pal.TextMuted).Render(strings.Repeat("─", leftWidth-2)))
	left.WriteString("\n")

	for i, r := range m.repos {
		line := m.formatRepoLine(r, leftWidth-4)
		if i == m.cursor {
			style := lipgloss.NewStyle().
				Background(pal.BgSelected).
				Bold(true).
				Width(leftWidth - 2)
			left.WriteString("  " + style.Render(line))
		} else {
			left.WriteString("  " + line)
		}
		left.WriteString("\n")
	}

	// Build right pane content
	var right strings.Builder
	if m.cursor < len(m.repos) {
		repo := m.repos[m.cursor]
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.AccentAlt)
		right.WriteString("  " + titleStyle.Render(fmt.Sprintf("git status — %s", repo.Name)))
		right.WriteString("\n")
		right.WriteString(lipgloss.NewStyle().Foreground(pal.TextMuted).Render(strings.Repeat("─", rightWidth-2)))
		right.WriteString("\n")
	}
	right.WriteString(m.viewport.View())

	// Search bar or help bar
	var footer string
	if m.searchMode {
		searchStyle := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
		footer = "  " + searchStyle.Render("/") + " " + m.searchInput.View()
	} else {
		helpStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)
		help := "  j/k: navigate • ←→/tab: cycle • gg/G: top/bottom • /: search • q: quit"
		footer = helpStyle.Render(help)
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		leftBorder.Render(left.String()),
		rightPane.Render(right.String()),
	)

	return body + "\n" + footer
}

func (m Model) leftPaneWidth() int {
	return min(max(m.width*30/100, 25), 40)
}

func (m Model) rightPaneWidth() int {
	return m.width - m.leftPaneWidth() - 1 // -1 for border
}

func (m Model) formatRepoLine(r RepoInfo, maxWidth int) string {
	if r.Error != "" {
		errStyle := lipgloss.NewStyle().Foreground(pal.Error)
		return fmt.Sprintf("%-*s %s", maxWidth/2, r.Name, errStyle.Render(r.Error))
	}

	branchStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)
	branch := r.Branch
	if len(branch) > 12 {
		branch = branch[:12] + "…"
	}

	// Changes summary
	var parts []string
	if r.Staged > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(pal.Success).Render(fmt.Sprintf("+%d", r.Staged)))
	}
	if r.Modified > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(pal.Error).Render(fmt.Sprintf("~%d", r.Modified)))
	}
	if r.Untracked > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(pal.TextMuted).Render(fmt.Sprintf("?%d", r.Untracked)))
	}

	changes := ""
	if len(parts) > 0 {
		changes = " " + strings.Join(parts, " ")
	} else if r.Clean {
		changes = " " + lipgloss.NewStyle().Foreground(pal.Success).Render("✓")
	}

	return fmt.Sprintf("%s %s%s", r.Name, branchStyle.Render(branch), changes)
}

func (m Model) currentStatusContent() string {
	content, ok := m.statusCache[m.cursor]
	if !ok {
		return lipgloss.NewStyle().Foreground(pal.TextMuted).Render("  Loading...")
	}

	if m.searchQuery != "" {
		return highlightSearch(content, m.searchQuery)
	}
	return content
}

// highlightSearch highlights matching lines in the content.
func highlightSearch(content, query string) string {
	if query == "" {
		return content
	}

	lowerQuery := strings.ToLower(query)
	lines := strings.Split(content, "\n")
	highlightStyle := lipgloss.NewStyle().Background(pal.Warning).Foreground(lipgloss.Color("0"))

	var result strings.Builder
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), lowerQuery) {
			result.WriteString(highlightStyle.Render(line))
		} else {
			result.WriteString(line)
		}
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}
	return result.String()
}

func (m Model) ensureStatus(idx int) tea.Cmd {
	if _, ok := m.statusCache[idx]; ok {
		return nil
	}
	return m.loadStatus(idx)
}

func (m Model) loadStatus(idx int) tea.Cmd {
	repo := m.repos[idx]
	return func() tea.Msg {
		ctx := context.Background()
		cmd := exec.CommandContext(ctx, "git", "-C", repo.Path, "-c", "color.status=always", "status")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return statusMsg{index: idx, output: fmt.Sprintf("Error: %s\n%s", err, string(output))}
		}
		return statusMsg{index: idx, output: string(output)}
	}
}
