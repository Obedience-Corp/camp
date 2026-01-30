package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/concept"
	"github.com/obediencecorp/camp/internal/editor"
)

// addStep represents the current step in the intent creation process.
type addStep int

const (
	addStepTitle addStep = iota
	addStepType
	addStepConcept
	addStepBody
	addStepDone
)

// AddOptions configures the IntentAddModel behavior.
type AddOptions struct {
	DefaultType string // Default intent type (e.g., "idea")
	FullMode    bool   // Include body textarea step
	Author      string // Auto-populated author (e.g., from git config)
}

// AddResult contains the collected intent data.
type AddResult struct {
	Title   string
	Type    string
	Concept string
	Body    string
	Author  string
}

// IntentAddModel is a BubbleTea model for creating new intents.
// It provides a step-based form: Title → Type → Concept → Body (optional).
type IntentAddModel struct {
	ctx        context.Context
	conceptSvc concept.Service

	step addStep

	// Title input
	titleInput textinput.Model

	// Type selection
	typeIdx int

	// Concept selection
	conceptPicker ConceptPickerModel

	// Body textarea (only used in full mode)
	bodyInput textarea.Model

	// Configuration
	fullMode    bool
	defaultType string
	author      string

	// Result state
	result    *AddResult
	cancelled bool

	// Display
	width  int
	height int

	// Vim mode state
	vimCmdMode    bool   // In command line mode (:)
	vimCmdBuf     string // Command buffer
	vimInsertMode bool   // In insert mode (typing text)
}

// intentTypes are the available intent types.
var intentTypes = []string{"idea", "feature", "bug", "research", "chore"}

// editorFinishedBodyMsg is sent when the external editor for body closes.
type editorFinishedBodyMsg struct {
	tmpPath string
	err     error
}

// NewIntentAddModel creates a new intent creation model.
func NewIntentAddModel(ctx context.Context, conceptSvc concept.Service, opts AddOptions) IntentAddModel {
	// Title input
	ti := textinput.New()
	ti.Placeholder = "What's the intent?"
	ti.CharLimit = 100
	ti.Width = 50
	ti.Focus()

	// Body textarea
	ta := textarea.New()
	ta.Placeholder = "Describe the intent (optional)..."
	ta.CharLimit = 2000
	ta.SetWidth(60)
	ta.SetHeight(6)

	// Find default type index
	typeIdx := 0
	if opts.DefaultType != "" {
		for i, t := range intentTypes {
			if t == opts.DefaultType {
				typeIdx = i
				break
			}
		}
	}

	return IntentAddModel{
		ctx:         ctx,
		conceptSvc:  conceptSvc,
		step:        addStepTitle,
		titleInput:  ti,
		typeIdx:     typeIdx,
		bodyInput:   ta,
		fullMode:    opts.FullMode,
		defaultType: opts.DefaultType,
		author:      opts.Author,
	}
}

// Init implements tea.Model.
func (m IntentAddModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (m IntentAddModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Check context cancellation first
	if err := m.ctx.Err(); err != nil {
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit
	}

	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.titleInput.Width = min(msg.Width-10, 80)
		w, h := m.calculateBodySize()
		m.bodyInput.SetWidth(w)
		m.bodyInput.SetHeight(h)
		return m, nil

	case editorFinishedBodyMsg:
		if msg.err == nil && msg.tmpPath != "" {
			if content, err := os.ReadFile(msg.tmpPath); err == nil {
				m.bodyInput.SetValue(string(content))
			}
			os.Remove(msg.tmpPath)
		}
		m.bodyInput.Focus()
		return m, textarea.Blink

	case tea.KeyMsg:
		switch m.step {
		case addStepTitle:
			return m.updateTitle(msg)
		case addStepType:
			return m.updateType(msg)
		case addStepConcept:
			return m.updateConcept(msg)
		case addStepBody:
			return m.updateBody(msg)
		}
	}

	return m, cmd
}

// updateTitle handles input during title step.
func (m IntentAddModel) updateTitle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit

	case "enter":
		title := strings.TrimSpace(m.titleInput.Value())
		if title == "" {
			// Don't proceed without a title
			return m, nil
		}
		m.step = addStepType
		m.titleInput.Blur()
		return m, nil
	}

	// Pass to text input
	var cmd tea.Cmd
	m.titleInput, cmd = m.titleInput.Update(msg)
	return m, cmd
}

// updateType handles input during type selection step.
func (m IntentAddModel) updateType(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit

	case "j", "down":
		if m.typeIdx < len(intentTypes)-1 {
			m.typeIdx++
		}
		return m, nil

	case "k", "up":
		if m.typeIdx > 0 {
			m.typeIdx--
		}
		return m, nil

	case "enter":
		// Move to concept selection
		m.step = addStepConcept
		m.conceptPicker = NewConceptPickerModel(m.ctx, m.conceptSvc)
		return m, nil
	}

	return m, nil
}

// updateConcept handles input during concept selection step.
func (m IntentAddModel) updateConcept(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit

	case "tab":
		// Skip concept selection
		return m.finishConceptStep()
	}

	// Pass to concept picker
	var cmd tea.Cmd
	m.conceptPicker, cmd = m.conceptPicker.Update(msg)

	if m.conceptPicker.Done() {
		if m.conceptPicker.Cancelled() {
			// Go back to type selection
			m.step = addStepType
			return m, nil
		}
		return m.finishConceptStep()
	}

	return m, cmd
}

// finishConceptStep completes the concept step and moves to body.
func (m IntentAddModel) finishConceptStep() (tea.Model, tea.Cmd) {
	m.step = addStepBody
	m.vimInsertMode = true // Start in insert mode for immediate typing
	w, h := m.calculateBodySize()
	m.bodyInput.SetWidth(w)
	m.bodyInput.SetHeight(h)
	m.bodyInput.Focus()
	return m, textarea.Blink
}

// calculateBodySize calculates the body textarea dimensions based on window size.
func (m IntentAddModel) calculateBodySize() (width, height int) {
	// Calculate reserved lines for the body step view:
	// - "Create Intent" title: 1 line
	// - Blank line after title: 1 line
	// - Progress summary: 3 lines (title, type, concept) + 2 blank lines = 5 lines max
	// - "Description (optional):" label: 1 line
	// - Vim command line: 2 lines (line + preceding newline, visible when active)
	// - Help text: 2 lines (line + preceding newline)
	const reservedLines = 1 + 1 + 5 + 1 + 2 + 2 // = 12 lines

	// Calculate dynamic height (minimum 6 lines, use available space)
	height = max(m.height-reservedLines, 6)
	// Cap at reasonable maximum to avoid excessive size
	height = min(height, 30)

	// Calculate dynamic width (cap at reasonable maximum for readability)
	width = max(m.width-4, 40) // Account for textarea borders/padding
	width = min(width, 120)

	return width, height
}

// updateBody handles input during body textarea step.
func (m IntentAddModel) updateBody(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle vim command mode first
	if m.vimCmdMode {
		return m.handleVimCommand(msg)
	}

	// Handle normal mode (navigation/commands)
	if !m.vimInsertMode {
		return m.handleVimNormal(msg)
	}

	// Insert mode handling
	switch msg.String() {
	case "ctrl+c":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit

	case "esc":
		// Exit insert mode, enter normal mode
		m.vimInsertMode = false
		return m, nil

	case "ctrl+s":
		// Save with body content
		return m.finishBodyStep()

	case "ctrl+e":
		// Open external editor
		return m, m.openExternalEditor()
	}

	// Pass to textarea (Enter inserts newline)
	var cmd tea.Cmd
	m.bodyInput, cmd = m.bodyInput.Update(msg)
	return m, cmd
}

// handleVimNormal handles keys in vim normal mode.
func (m IntentAddModel) handleVimNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	// Enter insert mode
	case "i":
		m.vimInsertMode = true
		return m, nil
	case "a":
		m.vimInsertMode = true
		// Move cursor right before entering insert (handled by textarea)
		return m, nil
	case "I":
		// Move to start of line and enter insert
		m.vimInsertMode = true
		return m, nil
	case "A":
		// Move to end of line and enter insert
		m.vimInsertMode = true
		return m, nil
	case "o":
		// Open line below and enter insert
		m.vimInsertMode = true
		m.bodyInput.SetValue(m.bodyInput.Value() + "\n")
		return m, nil
	case "O":
		// Open line above and enter insert
		m.vimInsertMode = true
		m.bodyInput.SetValue("\n" + m.bodyInput.Value())
		return m, nil

	// Navigation (basic vim motions)
	case "h", "left":
		var cmd tea.Cmd
		m.bodyInput, cmd = m.bodyInput.Update(tea.KeyMsg{Type: tea.KeyLeft})
		return m, cmd
	case "l", "right":
		var cmd tea.Cmd
		m.bodyInput, cmd = m.bodyInput.Update(tea.KeyMsg{Type: tea.KeyRight})
		return m, cmd
	case "j", "down":
		var cmd tea.Cmd
		m.bodyInput, cmd = m.bodyInput.Update(tea.KeyMsg{Type: tea.KeyDown})
		return m, cmd
	case "k", "up":
		var cmd tea.Cmd
		m.bodyInput, cmd = m.bodyInput.Update(tea.KeyMsg{Type: tea.KeyUp})
		return m, cmd

	// Command mode
	case ":":
		m.vimCmdMode = true
		m.vimCmdBuf = ""
		return m, nil

	// Quick save
	case "ctrl+s":
		return m.finishBodyStep()

	// External editor
	case "ctrl+e":
		return m, m.openExternalEditor()

	// Cancel
	case "ctrl+c":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit
	}

	return m, nil
}

// handleVimCommand processes vim-style commands like :wq, :q, :q!
func (m IntentAddModel) handleVimCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Execute command
		cmd := m.vimCmdBuf
		m.vimCmdMode = false
		m.vimCmdBuf = ""

		switch cmd {
		case "w", "wq":
			// Save with body
			return m.finishBodyStep()
		case "q":
			// Skip body, save intent without description
			m.bodyInput.SetValue("")
			return m.finishBodyStep()
		case "q!":
			// Cancel entire intent
			m.cancelled = true
			m.step = addStepDone
			return m, tea.Quit
		}
		return m, nil

	case tea.KeyEsc:
		// Cancel command mode
		m.vimCmdMode = false
		m.vimCmdBuf = ""
		return m, nil

	case tea.KeyBackspace:
		if len(m.vimCmdBuf) > 0 {
			m.vimCmdBuf = m.vimCmdBuf[:len(m.vimCmdBuf)-1]
		}
		if m.vimCmdBuf == "" {
			// Exit command mode if buffer is empty
			m.vimCmdMode = false
		}
		return m, nil

	case tea.KeyRunes:
		m.vimCmdBuf += string(msg.Runes)
		return m, nil
	}

	return m, nil
}

// openExternalEditor launches the user's editor with current body content.
func (m IntentAddModel) openExternalEditor() tea.Cmd {
	currentContent := m.bodyInput.Value()

	// Create temp file with current content
	tmpFile, err := os.CreateTemp("", "intent_body_*.md")
	if err != nil {
		return func() tea.Msg {
			return editorFinishedBodyMsg{err: err}
		}
	}
	tmpPath := tmpFile.Name()

	// Write current content to temp file
	if _, err := tmpFile.WriteString(currentContent); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return func() tea.Msg {
			return editorFinishedBodyMsg{err: err}
		}
	}
	tmpFile.Close()

	// Get editor and build command
	editorCmd := editor.GetEditor(context.Background())
	c := exec.Command(editorCmd, tmpPath)

	// Use tea.ExecProcess to properly handle the editor
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedBodyMsg{tmpPath: tmpPath, err: err}
	})
}

// finishBodyStep completes the body step and finishes creation.
func (m IntentAddModel) finishBodyStep() (tea.Model, tea.Cmd) {
	// Get concept path (may be empty if skipped)
	conceptPath := ""
	if m.conceptPicker.SelectedConcept() != nil {
		conceptPath = m.conceptPicker.SelectedPath()
	}

	m.result = &AddResult{
		Title:   strings.TrimSpace(m.titleInput.Value()),
		Type:    intentTypes[m.typeIdx],
		Concept: conceptPath,
		Body:    strings.TrimSpace(m.bodyInput.Value()),
		Author:  m.author,
	}
	m.step = addStepDone
	return m, tea.Quit
}

// View implements tea.Model.
func (m IntentAddModel) View() string {
	if m.step == addStepDone {
		return ""
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("Create Intent"))
	b.WriteString("\n\n")

	// Show progress summary
	b.WriteString(m.viewProgress())
	b.WriteString("\n")

	// Show current step
	switch m.step {
	case addStepTitle:
		b.WriteString(m.viewTitleStep())
	case addStepType:
		b.WriteString(m.viewTypeStep())
	case addStepConcept:
		b.WriteString(m.viewConceptStep())
	case addStepBody:
		b.WriteString(m.viewBodyStep())
	}

	return b.String()
}

// viewProgress shows the progress through steps.
func (m IntentAddModel) viewProgress() string {
	var parts []string

	// Title
	if m.step > addStepTitle {
		parts = append(parts, helpStyle.Render("Title: ")+intentTitleStyle.Render(m.titleInput.Value()))
	}

	// Type
	if m.step > addStepType {
		parts = append(parts, helpStyle.Render("Type: ")+intentTypeStyle.Render(intentTypes[m.typeIdx]))
	}

	// Concept (if selected)
	if m.step > addStepConcept && m.conceptPicker.SelectedPath() != "" {
		parts = append(parts, helpStyle.Render("Concept: ")+intentConceptStyle.Render(m.conceptPicker.SelectedPath()))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n") + "\n\n"
}

// viewTitleStep renders the title input step.
func (m IntentAddModel) viewTitleStep() string {
	var b strings.Builder

	b.WriteString("Title:\n")
	b.WriteString(m.titleInput.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("Enter: continue • Esc: cancel"))

	return b.String()
}

// viewTypeStep renders the type selection step.
func (m IntentAddModel) viewTypeStep() string {
	var b strings.Builder

	b.WriteString("Select type:\n")

	normalStyle := lipgloss.NewStyle()
	selectedStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Accent).
		Background(pal.BgSelected)

	for i, t := range intentTypes {
		cursor := "  "
		if i == m.typeIdx {
			cursor = "> "
		}
		line := cursor + t
		if i == m.typeIdx {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • Enter: select • Esc: cancel"))

	return b.String()
}

// viewConceptStep renders the concept picker step.
func (m IntentAddModel) viewConceptStep() string {
	var b strings.Builder

	b.WriteString(m.conceptPicker.View())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Tab: skip • Enter: select • Backspace: back • Esc: cancel"))

	return b.String()
}

// viewBodyStep renders the body textarea step.
func (m IntentAddModel) viewBodyStep() string {
	var b strings.Builder

	// Show mode indicator
	modeStr := "NORMAL"
	if m.vimInsertMode {
		modeStr = "INSERT"
	}
	if m.vimCmdMode {
		modeStr = "COMMAND"
	}
	b.WriteString(helpStyle.Render("Description (optional) — " + modeStr) + "\n")
	b.WriteString(m.bodyInput.View())
	b.WriteString("\n")

	// Show vim command buffer if in command mode
	if m.vimCmdMode {
		b.WriteString(":" + m.vimCmdBuf)
	}

	b.WriteString("\n")
	if m.vimInsertMode {
		b.WriteString(helpStyle.Render("Esc: normal mode • Ctrl+S: save • Ctrl+E: editor"))
	} else {
		b.WriteString(helpStyle.Render("i: insert • :wq: save • :q: skip • :q!: cancel • Ctrl+E: editor"))
	}

	return b.String()
}

// Done returns true if the model is finished.
func (m IntentAddModel) Done() bool {
	return m.step == addStepDone
}

// Cancelled returns true if the user cancelled.
func (m IntentAddModel) Cancelled() bool {
	return m.cancelled
}

// Result returns the collected intent data, or nil if cancelled.
func (m IntentAddModel) Result() *AddResult {
	return m.result
}
