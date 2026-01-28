package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/concept"
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
}

// AddResult contains the collected intent data.
type AddResult struct {
	Title   string
	Type    string
	Concept string
	Body    string
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

	// Result state
	result    *AddResult
	cancelled bool

	// Display
	width  int
	height int
}

// intentTypes are the available intent types.
var intentTypes = []string{"idea", "feature", "bug", "research", "chore"}

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
		m.titleInput.Width = min(msg.Width-10, 60)
		m.bodyInput.SetWidth(min(msg.Width-10, 70))
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
		return m.finishConceptStep("")
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
		return m.finishConceptStep(m.conceptPicker.SelectedPath())
	}

	return m, cmd
}

// finishConceptStep completes the concept step and moves to the next.
func (m IntentAddModel) finishConceptStep(conceptPath string) (tea.Model, tea.Cmd) {
	if m.fullMode {
		m.step = addStepBody
		m.bodyInput.Focus()
		return m, textarea.Blink
	}

	// Fast mode: skip body, finish immediately
	m.result = &AddResult{
		Title:   strings.TrimSpace(m.titleInput.Value()),
		Type:    intentTypes[m.typeIdx],
		Concept: conceptPath,
		Body:    "",
	}
	m.step = addStepDone
	return m, tea.Quit
}

// updateBody handles input during body textarea step.
func (m IntentAddModel) updateBody(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit

	case "tab":
		// Skip body, finish
		return m.finishBodyStep()

	case "ctrl+d":
		// Done with body
		return m.finishBodyStep()
	}

	// Pass to textarea
	var cmd tea.Cmd
	m.bodyInput, cmd = m.bodyInput.Update(msg)
	return m, cmd
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

	b.WriteString("Description (optional):\n")
	b.WriteString(m.bodyInput.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("Tab: skip • Ctrl+D: done • Esc: cancel"))

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
