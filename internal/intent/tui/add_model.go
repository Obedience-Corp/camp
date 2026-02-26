package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/editor"
	"github.com/Obedience-Corp/camp/internal/intent/tui/vim"
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
	DefaultType  string            // Default intent type (e.g., "idea")
	FullMode     bool              // Include body textarea step
	Author       string            // Auto-populated author (e.g., from git config)
	CampaignRoot string            // Campaign root for @ completion
	Shortcuts    map[string]string // Navigation shortcuts (key → campaign-relative path, e.g., "de" → "workflow/design/")
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
	fullMode     bool
	defaultType  string
	author       string
	campaignRoot string
	shortcuts    map[string]string // key → campaign-relative path

	// Completion state
	completion completionState

	// Result state
	result       *AddResult
	savedResults []*AddResult
	cancelled    bool
	savedCount   int

	// Display
	width  int
	height int
}

// intentTypes are the available intent types.
var intentTypes = []string{"idea", "feature", "bug", "research", "chore", "feedback"}

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
	vimEd.SetSyntax(vim.NewMarkdownStyler())

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
		ctx:          ctx,
		conceptSvc:   conceptSvc,
		step:         addStepTitle,
		titleInput:   ti,
		typeIdx:      typeIdx,
		vimEditor:    vimEd,
		fullMode:     opts.FullMode,
		defaultType:  opts.DefaultType,
		author:       opts.Author,
		campaignRoot: opts.CampaignRoot,
		shortcuts:    opts.Shortcuts,
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
	// Handle completion popup when active
	if m.completion.active {
		switch msg.String() {
		case "tab", "down":
			if len(m.completion.candidates) > 0 {
				m.completion.selected = (m.completion.selected + 1) % len(m.completion.candidates)
			}
			return m, nil
		case "shift+tab", "up":
			if len(m.completion.candidates) > 0 {
				m.completion.selected = (m.completion.selected - 1 + len(m.completion.candidates)) % len(m.completion.candidates)
			}
			return m, nil
		case "enter":
			m.acceptTitleCompletion()
			return m, nil
		case "esc":
			m.completion.active = false
			return m, nil
		}
	}

	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit

	case "ctrl+n":
		// Quick save: title-only intent, then reset for next
		if strings.TrimSpace(m.titleInput.Value()) != "" {
			return m.saveAndReset()
		}
		return m, nil

	case "enter":
		if m.completion.active {
			m.acceptTitleCompletion()
			return m, nil
		}
		title := strings.TrimSpace(m.titleInput.Value())
		if title == "" {
			return m, nil
		}
		m.step = addStepType
		m.titleInput.Blur()
		return m, nil
	}

	// Pass to text input
	var cmd tea.Cmd
	m.titleInput, cmd = m.titleInput.Update(msg)

	// Auto-expand shortcut on '/' key (e.g., @de/ → @workflow/design/)
	m.autoExpandTitleShortcut()

	// Update completion state for title input
	m.updateTitleCompletion()

	return m, cmd
}

// updateTitleCompletion checks for @ patterns in the title input.
func (m *IntentAddModel) updateTitleCompletion() {
	if m.campaignRoot == "" {
		m.completion.active = false
		return
	}

	val := m.titleInput.Value()
	pos := m.titleInput.Position()
	query, atCol := extractAtQuery(val, pos)
	if atCol < 0 {
		m.completion.active = false
		return
	}

	candidates := atCompletionCandidates(query, m.campaignRoot, m.shortcuts)
	if len(candidates) == 0 {
		m.completion.active = false
		return
	}

	m.completion.active = true
	m.completion.query = query
	m.completion.atOffset = atCol
	m.completion.candidates = candidates
	if m.completion.selected >= len(candidates) {
		m.completion.selected = 0
	}
}

// autoExpandTitleShortcut checks if the user just typed '/' after a shortcut key
// in the title input and expands it to the real path inline.
func (m *IntentAddModel) autoExpandTitleShortcut() {
	if len(m.shortcuts) == 0 {
		return
	}

	val := m.titleInput.Value()
	pos := m.titleInput.Position()
	if pos == 0 || pos > len(val) {
		return
	}
	// The character just typed must be '/'
	if val[pos-1] != '/' {
		return
	}

	// Walk backwards from pos-1 to find @
	for i := pos - 2; i >= 0; i-- {
		ch := val[i]
		if ch == '@' {
			key := val[i+1 : pos-1] // text between @ and /
			if expanded, ok := m.shortcuts[key]; ok {
				// Replace @key/ with @path
				newVal := val[:i] + "@" + expanded + val[pos:]
				newCursor := i + 1 + len(expanded)
				m.titleInput.SetValue(newVal)
				m.titleInput.SetCursor(newCursor)
			}
			return
		}
		if ch == ' ' || ch == '\t' {
			return
		}
	}
}

// acceptTitleCompletion inserts the selected completion into the title input.
func (m *IntentAddModel) acceptTitleCompletion() {
	if !m.completion.active || len(m.completion.candidates) == 0 {
		return
	}

	selected := m.completion.candidates[m.completion.selected]
	val := m.titleInput.Value()
	pos := m.titleInput.Position()

	// Rebuild value: before @ + selected + after cursor
	newVal := val[:m.completion.atOffset] + selected + val[pos:]
	newCursor := m.completion.atOffset + len(selected)
	m.titleInput.SetValue(newVal)
	m.titleInput.SetCursor(newCursor)

	m.completion.active = false
}

// updateType handles input during type selection step.
func (m IntentAddModel) updateType(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit

	case "ctrl+n":
		return m.saveAndReset()

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

	case "ctrl+n":
		return m.saveAndReset()

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

	// Handle ctrl+n for save-and-new (always available)
	if msg.String() == "ctrl+n" {
		return m.saveAndReset()
	}

	// Handle ctrl+s for quick save (always available)
	if msg.String() == "ctrl+s" {
		return m.finishBodyStep()
	}

	// Handle completion popup when active
	if m.completion.active {
		switch msg.String() {
		case "tab", "down":
			if len(m.completion.candidates) > 0 {
				m.completion.selected = (m.completion.selected + 1) % len(m.completion.candidates)
			}
			return m, nil
		case "shift+tab", "up":
			if len(m.completion.candidates) > 0 {
				m.completion.selected = (m.completion.selected - 1 + len(m.completion.candidates)) % len(m.completion.candidates)
			}
			return m, nil
		case "enter":
			m.acceptCompletion()
			return m, nil
		case "esc":
			m.completion.active = false
			return m, nil
		}
	}

	// Pass to vim editor
	cmd, _ := m.vimEditor.Update(msg)

	// Handle command results from vim editor
	switch cmd {
	case "w", "wq":
		return m.finishBodyStep()
	case "q":
		return m, nil
	case "q!":
		m.cancelled = true
		m.step = addStepDone
		return m, tea.Quit
	}

	// Auto-expand shortcut on '/' key (e.g., @de/ → @workflow/design/)
	m.autoExpandBodyShortcut()

	// After vim processes the key, update completion state
	m.updateCompletion()

	return m, nil
}

// updateCompletion checks whether the cursor is in an @ word and updates candidates.
func (m *IntentAddModel) updateCompletion() {
	if m.vimEditor.Mode() != vim.ModeInsert || m.campaignRoot == "" {
		m.completion.active = false
		return
	}

	cur := m.vimEditor.Cursor()
	content := m.vimEditor.Content()
	if content == "" {
		m.completion.active = false
		return
	}

	lines := strings.Split(content, "\n")
	if cur.Line >= len(lines) {
		m.completion.active = false
		return
	}

	line := lines[cur.Line]
	query, atCol := extractAtQuery(line, cur.Col)
	if atCol < 0 {
		m.completion.active = false
		return
	}

	candidates := atCompletionCandidates(query, m.campaignRoot, m.shortcuts)
	if len(candidates) == 0 {
		m.completion.active = false
		return
	}

	// Update completion state
	m.completion.active = true
	m.completion.query = query
	m.completion.atOffset = atCol
	m.completion.candidates = candidates
	if m.completion.selected >= len(candidates) {
		m.completion.selected = 0
	}
}

// autoExpandBodyShortcut checks if the user just typed '/' after a shortcut key
// in the body vim editor and expands it to the real path inline.
func (m *IntentAddModel) autoExpandBodyShortcut() {
	if len(m.shortcuts) == 0 || m.vimEditor.Mode() != vim.ModeInsert {
		return
	}

	cur := m.vimEditor.Cursor()
	content := m.vimEditor.Content()
	if content == "" {
		return
	}

	lines := strings.Split(content, "\n")
	if cur.Line >= len(lines) {
		return
	}

	line := lines[cur.Line]
	col := cur.Col
	if col == 0 || col > len(line) {
		return
	}
	if line[col-1] != '/' {
		return
	}

	// Walk backwards to find @
	for i := col - 2; i >= 0; i-- {
		ch := line[i]
		if ch == '@' {
			key := line[i+1 : col-1]
			if expanded, ok := m.shortcuts[key]; ok {
				newLine := line[:i] + "@" + expanded + line[col:]
				lines[cur.Line] = newLine
				m.vimEditor.SetContent(strings.Join(lines, "\n"))
				newCol := i + 1 + len(expanded)
				m.vimEditor.SetCursorInsert(vim.Position{Line: cur.Line, Col: newCol})
			}
			return
		}
		if ch == ' ' || ch == '\t' {
			return
		}
	}
}

// acceptCompletion inserts the selected completion into the editor.
func (m *IntentAddModel) acceptCompletion() {
	if !m.completion.active || len(m.completion.candidates) == 0 {
		return
	}

	selected := m.completion.candidates[m.completion.selected]
	cur := m.vimEditor.Cursor()

	content := m.vimEditor.Content()
	lines := strings.Split(content, "\n")
	if cur.Line >= len(lines) {
		m.completion.active = false
		return
	}

	line := lines[cur.Line]
	// Rebuild line: before @ + selected + after cursor
	newLine := line[:m.completion.atOffset] + selected + line[cur.Col:]
	lines[cur.Line] = newLine
	m.vimEditor.SetContent(strings.Join(lines, "\n"))

	// Position cursor after the inserted text
	newCol := m.completion.atOffset + len(selected)
	m.vimEditor.SetCursorInsert(vim.Position{Line: cur.Line, Col: newCol})

	m.completion.active = false
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

// collectCurrentResult builds an AddResult from the model's current state.
func (m IntentAddModel) collectCurrentResult() *AddResult {
	title := strings.TrimSpace(m.titleInput.Value())
	if title == "" {
		return nil
	}

	conceptPath := ""
	if m.step > addStepConcept && m.conceptPicker.SelectedConcept() != nil {
		conceptPath = m.conceptPicker.SelectedPath()
	}

	body := ""
	if m.step >= addStepBody {
		body = strings.TrimSpace(m.vimEditor.Content())
	}

	return &AddResult{
		Title:   title,
		Type:    intentTypes[m.typeIdx],
		Concept: conceptPath,
		Body:    body,
		Author:  m.author,
	}
}

// saveAndReset saves the current intent to the accumulated list and resets the form.
func (m IntentAddModel) saveAndReset() (tea.Model, tea.Cmd) {
	result := m.collectCurrentResult()
	if result == nil {
		return m, nil
	}

	m.savedResults = append(m.savedResults, result)
	m.savedCount++

	// Reset form to fresh state
	m.titleInput.Reset()
	m.titleInput.Focus()
	m.step = addStepTitle

	// Reset type to default
	m.typeIdx = 0
	if m.defaultType != "" {
		for i, t := range intentTypes {
			if t == m.defaultType {
				m.typeIdx = i
				break
			}
		}
	}

	// Reset concept picker (will be recreated on step entry)
	m.conceptPicker = ConceptPickerModel{}

	// Reset vim editor
	m.vimEditor.SetContent("")

	return m, textinput.Blink
}

// SavedResults returns intents saved via Ctrl-n during the session.
func (m IntentAddModel) SavedResults() []*AddResult {
	return m.savedResults
}

// View implements tea.Model.
func (m IntentAddModel) View() string {
	if m.step == addStepDone {
		return ""
	}

	var b strings.Builder

	b.WriteString(TitleStyle.Render("Create Intent"))
	if m.savedCount > 0 {
		b.WriteString(SuccessStyle.Render(fmt.Sprintf("  [%d saved]", m.savedCount)))
	}
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
	b.WriteString("\n")

	// Show completion popup if active
	if popup := completionView(&m.completion); popup != "" {
		b.WriteString(popup)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("Enter: continue • Ctrl+N: save & new • Esc: cancel"))

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
	b.WriteString(HelpStyle.Render("j/k: navigate • Enter: select • Ctrl+N: save & new • Esc: cancel"))

	return b.String()
}

// viewConceptStep renders the concept picker step.
func (m IntentAddModel) viewConceptStep() string {
	var b strings.Builder

	b.WriteString(m.conceptPicker.View())
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("Tab: skip • Enter: select • Ctrl+N: save & new • Esc: cancel"))

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

	// Show completion popup if active
	if popup := completionView(&m.completion); popup != "" {
		b.WriteString(popup)
		b.WriteString("\n")
	}

	// Show vim command buffer if in command mode
	if m.vimEditor.IsCommandMode() {
		b.WriteString(":" + m.vimEditor.CommandBuffer())
	}

	b.WriteString("\n")

	// Context-sensitive help
	switch m.vimEditor.Mode() {
	case vim.ModeInsert:
		b.WriteString(HelpStyle.Render("Esc: normal mode • Ctrl+S: save • Ctrl+N: save & new • Ctrl+E: editor"))
	case vim.ModeVisual, vim.ModeVisualLine:
		b.WriteString(HelpStyle.Render("d: delete • y: yank • c: change • Esc: normal"))
	case vim.ModeCommand:
		b.WriteString(HelpStyle.Render(":wq save • :q! cancel • Esc: back"))
	default:
		b.WriteString(HelpStyle.Render("i: insert • v: visual • Enter/:wq: save • Ctrl+N: new • Ctrl+E: editor"))
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
