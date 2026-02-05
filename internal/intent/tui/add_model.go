package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/concept"
	"github.com/obediencecorp/camp/internal/editor"
	"github.com/obediencecorp/camp/internal/intent/tui/vim"
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

	// Body vim editor (only used in full mode)
	vimEditor *vim.Editor

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

	// Body vim editor
	vimEd := vim.NewEditor("")
	vimEd.SetSize(60, 6)

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
		vimEditor:   vimEd,
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
		m.vimEditor.SetSize(w, h)
		return m, nil

	case editorFinishedBodyMsg:
		if msg.err == nil && msg.tmpPath != "" {
			if content, err := os.ReadFile(msg.tmpPath); err == nil {
				m.vimEditor.SetContent(string(content))
			}
			os.Remove(msg.tmpPath)
		}
		return m, nil

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
	w, h := m.calculateBodySize()
	m.vimEditor.SetSize(w, h)
	// Start in insert mode for immediate typing
	m.vimEditor.State().EnterInsert()
	return m, nil
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

// updateBody handles input during body vim editor step.
func (m IntentAddModel) updateBody(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle ctrl+c for cancel (always available)
	if msg.String() == "ctrl+c" {
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit
	}

	// Handle ctrl+e for external editor (always available)
	if msg.String() == "ctrl+e" {
		return m, m.openExternalEditor()
	}

	// Handle ctrl+s for quick save (always available)
	if msg.String() == "ctrl+s" {
		return m.finishBodyStep()
	}

	// Pass to vim editor
	cmd, _ := m.vimEditor.Update(msg)

	// Handle command results from vim editor
	switch cmd {
	case "w", "wq":
		return m.finishBodyStep()
	case "q":
		// :q does nothing - body is required
		// Just return to normal mode (already happened in vim editor)
		return m, nil
	case "q!":
		// Cancel entire intent
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit
	}

	return m, nil
}

// openExternalEditor launches the user's editor with current body content.
func (m IntentAddModel) openExternalEditor() tea.Cmd {
	currentContent := m.vimEditor.Content()

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
		Body:    strings.TrimSpace(m.vimEditor.Content()),
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

	b.WriteString(TitleStyle.Render("Create Intent"))
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
		parts = append(parts, HelpStyle.Render("Title: ")+IntentTitleStyle.Render(m.titleInput.Value()))
	}

	// Type
	if m.step > addStepType {
		parts = append(parts, HelpStyle.Render("Type: ")+IntentTypeStyle.Render(intentTypes[m.typeIdx]))
	}

	// Concept (if selected)
	if m.step > addStepConcept && m.conceptPicker.SelectedPath() != "" {
		parts = append(parts, HelpStyle.Render("Concept: ")+IntentConceptStyle.Render(m.conceptPicker.SelectedPath()))
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
	b.WriteString(HelpStyle.Render("Enter: continue • Esc: cancel"))

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
	b.WriteString(HelpStyle.Render("j/k: navigate • Enter: select • Esc: cancel"))

	return b.String()
}

// viewConceptStep renders the concept picker step.
func (m IntentAddModel) viewConceptStep() string {
	var b strings.Builder

	b.WriteString(m.conceptPicker.View())
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("Tab: skip • Enter: select • Backspace: back • Esc: cancel"))

	return b.String()
}

// viewBodyStep renders the body vim editor step.
func (m IntentAddModel) viewBodyStep() string {
	var b strings.Builder

	// Show mode indicator
	modeStr := m.vimEditor.Mode().String()
	b.WriteString(HelpStyle.Render("Description (optional) — "+modeStr) + "\n")

	// Render vim editor
	cfg := vim.DefaultViewConfig()
	b.WriteString(m.vimEditor.View(cfg))
	b.WriteString("\n")

	// Show vim command buffer if in command mode
	if m.vimEditor.IsCommandMode() {
		b.WriteString(":" + m.vimEditor.CommandBuffer())
	}

	b.WriteString("\n")

	// Context-sensitive help
	switch m.vimEditor.Mode() {
	case vim.ModeInsert:
		b.WriteString(HelpStyle.Render("Esc: normal mode • Ctrl+S: save • Ctrl+E: editor"))
	case vim.ModeVisual, vim.ModeVisualLine:
		b.WriteString(HelpStyle.Render("d: delete • y: yank • c: change • Esc: normal"))
	case vim.ModeCommand:
		b.WriteString(HelpStyle.Render(":wq save • :q! cancel • Esc: back"))
	default:
		b.WriteString(HelpStyle.Render("i: insert • v: visual • Enter/:wq: save • Ctrl+E: editor"))
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
