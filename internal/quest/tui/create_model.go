//go:build dev

package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Obedience-Corp/camp/internal/editor"
	"github.com/Obedience-Corp/camp/internal/tui/vim"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// createStep represents the current step in the quest creation process.
type createStep int

const (
	createStepName createStep = iota
	createStepPurpose
	createStepDescription
	createStepTags
	createStepDone
)

// CreateOptions configures the QuestCreateModel behavior.
type CreateOptions struct {
	DefaultName    string
	DefaultPurpose string
	DefaultTags    string
}

// CreateResult contains the collected quest data.
type CreateResult struct {
	Name        string
	Purpose     string
	Description string
	Tags        string
}

// editorFinishedMsg is sent when the external editor closes.
type editorFinishedMsg struct {
	tmpPath string
	err     error
}

// QuestCreateModel is a BubbleTea model for creating new quests.
// It provides a step-based form: Name -> Purpose -> Description (vim) -> Tags.
type QuestCreateModel struct {
	ctx context.Context

	step createStep

	nameInput    textinput.Model
	purposeInput textinput.Model
	tagsInput    textinput.Model
	vimEditor    *vim.Editor

	result    *CreateResult
	cancelled bool
	nameErr   string

	width  int
	height int
}

// NewQuestCreateModel creates a new quest creation model.
func NewQuestCreateModel(ctx context.Context, opts CreateOptions) QuestCreateModel {
	ni := textinput.New()
	ni.Placeholder = "Quest name (slug-friendly)"
	ni.CharLimit = 80
	ni.Width = 50
	ni.Focus()
	if opts.DefaultName != "" {
		ni.SetValue(opts.DefaultName)
	}

	pi := textinput.New()
	pi.Placeholder = "Short purpose statement (optional)"
	pi.CharLimit = 200
	pi.Width = 60
	if opts.DefaultPurpose != "" {
		pi.SetValue(opts.DefaultPurpose)
	}

	ti := textinput.New()
	ti.Placeholder = "Comma-separated tags (optional)"
	ti.CharLimit = 200
	ti.Width = 60
	if opts.DefaultTags != "" {
		ti.SetValue(opts.DefaultTags)
	}

	vimEd := vim.NewEditor("")
	vimEd.SetSize(60, 6)
	vimEd.SetSyntax(vim.NewMarkdownStyler())

	return QuestCreateModel{
		ctx:          ctx,
		step:         createStepName,
		nameInput:    ni,
		purposeInput: pi,
		tagsInput:    ti,
		vimEditor:    vimEd,
	}
}

// Init implements tea.Model.
func (m QuestCreateModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (m QuestCreateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if err := m.ctx.Err(); err != nil {
		m.cancelled = true
		m.step = createStepDone
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.nameInput.Width = min(msg.Width-10, 80)
		m.purposeInput.Width = min(msg.Width-10, 80)
		m.tagsInput.Width = min(msg.Width-10, 80)
		w, h := m.calculateDescriptionSize()
		m.vimEditor.SetSize(w, h)
		return m, nil

	case editorFinishedMsg:
		if msg.err == nil && msg.tmpPath != "" {
			if content, err := os.ReadFile(msg.tmpPath); err == nil {
				m.vimEditor.SetContent(string(content))
			}
			os.Remove(msg.tmpPath)
		}
		return m, nil

	case tea.KeyMsg:
		switch m.step {
		case createStepName:
			return m.updateName(msg)
		case createStepPurpose:
			return m.updatePurpose(msg)
		case createStepDescription:
			return m.updateDescription(msg)
		case createStepTags:
			return m.updateTags(msg)
		}
	}

	return m, nil
}

func (m QuestCreateModel) updateName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelled = true
		m.step = createStepDone
		return m, tea.Quit

	case "enter":
		name := strings.TrimSpace(m.nameInput.Value())
		if name == "" {
			m.nameErr = "Quest name is required"
			return m, nil
		}
		m.nameErr = ""
		m.step = createStepPurpose
		m.nameInput.Blur()
		m.purposeInput.Focus()
		return m, textinput.Blink
	}

	m.nameErr = ""
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m QuestCreateModel) updatePurpose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelled = true
		m.step = createStepDone
		return m, tea.Quit

	case "enter":
		m.step = createStepDescription
		m.purposeInput.Blur()
		w, h := m.calculateDescriptionSize()
		m.vimEditor.SetSize(w, h)
		m.vimEditor.State().EnterInsert()
		return m, nil
	}

	var cmd tea.Cmd
	m.purposeInput, cmd = m.purposeInput.Update(msg)
	return m, cmd
}

func (m QuestCreateModel) updateDescription(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.cancelled = true
		m.step = createStepDone
		return m, tea.Quit
	}

	if msg.String() == "ctrl+e" {
		return m, m.openExternalEditor()
	}

	if msg.String() == "ctrl+s" {
		return m.advanceToTags()
	}

	if msg.String() == "tab" && m.vimEditor.Mode() != vim.ModeInsert {
		return m.advanceToTags()
	}

	cmd, _ := m.vimEditor.Update(msg)

	switch cmd {
	case "w", "wq":
		return m.advanceToTags()
	case "q":
		return m, nil
	case "q!":
		m.cancelled = true
		m.step = createStepDone
		return m, tea.Quit
	}

	return m, nil
}

func (m QuestCreateModel) updateTags(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelled = true
		m.step = createStepDone
		return m, tea.Quit

	case "enter":
		return m.finish()
	}

	var cmd tea.Cmd
	m.tagsInput, cmd = m.tagsInput.Update(msg)
	return m, cmd
}

func (m QuestCreateModel) advanceToTags() (QuestCreateModel, tea.Cmd) {
	m.step = createStepTags
	m.tagsInput.Focus()
	return m, textinput.Blink
}

func (m QuestCreateModel) finish() (QuestCreateModel, tea.Cmd) {
	m.result = &CreateResult{
		Name:        strings.TrimSpace(m.nameInput.Value()),
		Purpose:     strings.TrimSpace(m.purposeInput.Value()),
		Description: strings.TrimSpace(m.vimEditor.Content()),
		Tags:        strings.TrimSpace(m.tagsInput.Value()),
	}
	m.step = createStepDone
	return m, tea.Quit
}

func (m QuestCreateModel) calculateDescriptionSize() (width, height int) {
	// Reserved: title(1) + blank(1) + progress(~4) + label(1) + vim cmd(2) + help(2) = 11
	const reservedLines = 11
	height = max(m.height-reservedLines, 6)
	height = min(height, 30)
	width = max(m.width-4, 40)
	width = min(width, 120)
	return width, height
}

func (m QuestCreateModel) openExternalEditor() tea.Cmd {
	currentContent := m.vimEditor.Content()

	tmpFile, err := os.CreateTemp("", "quest_desc_*.md")
	if err != nil {
		return func() tea.Msg {
			return editorFinishedMsg{err: err}
		}
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(currentContent); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return func() tea.Msg {
			return editorFinishedMsg{err: err}
		}
	}
	tmpFile.Close()

	editorCmd := editor.GetEditor(context.Background())
	c := exec.Command(editorCmd, tmpPath)

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{tmpPath: tmpPath, err: err}
	})
}

// View implements tea.Model.
func (m QuestCreateModel) View() string {
	if m.step == createStepDone {
		return ""
	}

	var b strings.Builder

	b.WriteString(TitleStyle.Render("Create Quest"))
	b.WriteString("\n\n")

	b.WriteString(m.viewProgress())

	switch m.step {
	case createStepName:
		b.WriteString(m.viewNameStep())
	case createStepPurpose:
		b.WriteString(m.viewPurposeStep())
	case createStepDescription:
		b.WriteString(m.viewDescriptionStep())
	case createStepTags:
		b.WriteString(m.viewTagsStep())
	}

	return b.String()
}

func (m QuestCreateModel) viewProgress() string {
	var parts []string

	if m.step > createStepName {
		parts = append(parts, FieldLabelStyle.Render("Name: ")+FieldValueStyle.Render(m.nameInput.Value()))
	}
	if m.step > createStepPurpose {
		purpose := m.purposeInput.Value()
		if purpose == "" {
			purpose = "(none)"
		}
		parts = append(parts, FieldLabelStyle.Render("Purpose: ")+FieldValueStyle.Render(purpose))
	}
	if m.step > createStepDescription {
		desc := m.vimEditor.Content()
		if desc == "" {
			desc = "(none)"
		} else if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		parts = append(parts, FieldLabelStyle.Render("Description: ")+FieldValueStyle.Render(desc))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n") + "\n\n"
}

func (m QuestCreateModel) viewNameStep() string {
	var b strings.Builder
	b.WriteString("Name:\n")
	b.WriteString(m.nameInput.View())
	b.WriteString("\n")
	if m.nameErr != "" {
		b.WriteString(ErrorStyle.Render(m.nameErr))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("Enter: continue • Esc: cancel"))
	return b.String()
}

func (m QuestCreateModel) viewPurposeStep() string {
	var b strings.Builder
	b.WriteString("Purpose:\n")
	b.WriteString(m.purposeInput.View())
	b.WriteString("\n\n")
	b.WriteString(HelpStyle.Render("Enter: continue • Esc: cancel"))
	return b.String()
}

func (m QuestCreateModel) viewDescriptionStep() string {
	var b strings.Builder

	modeStr := m.vimEditor.Mode().String()
	b.WriteString(HelpStyle.Render(fmt.Sprintf("Description (optional) — %s", modeStr)) + "\n")

	cfg := vim.DefaultViewConfig()
	b.WriteString(m.vimEditor.View(cfg))
	b.WriteString("\n")

	if m.vimEditor.IsCommandMode() {
		b.WriteString(":" + m.vimEditor.CommandBuffer())
	}

	b.WriteString("\n")

	switch m.vimEditor.Mode() {
	case vim.ModeInsert:
		b.WriteString(HelpStyle.Render("Esc: normal mode • Ctrl+S: save • Ctrl+E: editor"))
	case vim.ModeVisual, vim.ModeVisualLine:
		b.WriteString(HelpStyle.Render("d: delete • y: yank • c: change • Esc: normal"))
	case vim.ModeCommand:
		b.WriteString(HelpStyle.Render(":wq save • :q! cancel • Esc: back"))
	default:
		b.WriteString(HelpStyle.Render("i: insert • Tab/:wq: next • Ctrl+E: editor • Ctrl+C: cancel"))
	}

	return b.String()
}

func (m QuestCreateModel) viewTagsStep() string {
	var b strings.Builder
	b.WriteString("Tags:\n")
	b.WriteString(m.tagsInput.View())
	b.WriteString("\n\n")
	b.WriteString(HelpStyle.Render("Enter: create quest • Esc: cancel"))
	return b.String()
}

// Done returns true if the model is finished.
func (m QuestCreateModel) Done() bool {
	return m.step == createStepDone
}

// Cancelled returns true if the user cancelled.
func (m QuestCreateModel) Cancelled() bool {
	return m.cancelled
}

// Result returns the collected quest data, or nil if cancelled.
func (m QuestCreateModel) Result() *CreateResult {
	return m.result
}
