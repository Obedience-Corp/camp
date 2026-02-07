package explorer

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/tui"
)

// creationStep represents the current step in new intent creation.
type creationStep int

const (
	stepTitle creationStep = iota
	stepType
	stepConcept
)

// createTypeOptions are the available types for new intents.
var createTypeOptions = []string{"idea", "feature", "bug", "research", "chore", "feedback"}

// updateCreating handles key input during new intent creation.
func (m *Model) updateCreating(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.String() {
	case "esc":
		// Cancel creation, return to list
		m.focus = focusList
		m.titleInput.Blur()
		return m, nil
	}

	switch m.creationStep {
	case stepTitle:
		switch msg.String() {
		case "enter":
			if m.titleInput.Value() != "" {
				m.creationStep = stepType
				m.titleInput.Blur()
			}
			return m, nil
		}
		m.titleInput, cmd = m.titleInput.Update(msg)
		return m, cmd

	case stepType:
		switch msg.String() {
		case "j", "down":
			if m.createTypeIdx < len(createTypeOptions)-1 {
				m.createTypeIdx++
			}
		case "k", "up":
			if m.createTypeIdx > 0 {
				m.createTypeIdx--
			}
		case "enter":
			// Move to concept selection
			m.creationStep = stepConcept
			m.conceptPicker = tui.NewConceptPickerModel(m.ctx, m.conceptSvc)
			return m, nil
		}
		return m, nil

	case stepConcept:
		switch msg.String() {
		case "tab":
			// Skip concept selection, create intent without concept
			return m.finishIntentCreation("")
		}
		// Pass to concept picker
		m.conceptPicker, cmd = m.conceptPicker.Update(msg)
		if m.conceptPicker.Done() {
			if m.conceptPicker.Cancelled() {
				// Go back to type selection
				m.creationStep = stepType
				return m, nil
			}
			// Create intent with selected concept
			return m.finishIntentCreation(m.conceptPicker.SelectedPath())
		}
		return m, cmd
	}

	return m, nil
}

// finishIntentCreation creates the intent and returns to list view.
func (m *Model) finishIntentCreation(conceptPath string) (tea.Model, tea.Cmd) {
	title := m.titleInput.Value()
	intentType := intent.Type(createTypeOptions[m.createTypeIdx])

	opts := intent.CreateOptions{
		Title:   title,
		Type:    intentType,
		Concept: conceptPath,
	}

	_, err := m.service.CreateDirect(m.ctx, opts)
	if err != nil {
		m.statusMessage = "Error creating intent: " + err.Error()
		m.focus = focusList
		return m, nil
	}

	// Auto-commit the creation using shared helper
	if m.campaignRoot != "" && m.campaignID != "" {
		_ = git.IntentCommitAll(m.ctx, git.IntentCommitOptions{
			CampaignRoot: m.campaignRoot,
			CampaignID:   m.campaignID,
			Action:       git.IntentActionCreate,
			IntentTitle:  title,
		})
	}

	m.statusMessage = "Intent created: " + title
	m.focus = focusList

	// Reload intents
	return m, m.loadIntents()
}

// viewCreating renders the new intent creation form.
func (m *Model) viewCreating() string {
	var b strings.Builder

	b.WriteString(tui.TitleStyle.Render("Create New Intent"))
	b.WriteString("\n\n")

	switch m.creationStep {
	case stepTitle:
		b.WriteString("Enter title:\n")
		b.WriteString(m.titleInput.View())
		b.WriteString("\n\n")
		b.WriteString(tui.HelpStyle.Render("Enter: continue . Esc: cancel"))

	case stepType:
		b.WriteString("Title: " + m.titleInput.Value() + "\n\n")
		b.WriteString("Select type:\n")
		for i, t := range createTypeOptions {
			cursor := "  "
			if i == m.createTypeIdx {
				cursor = "> "
			}
			b.WriteString(cursor + t + "\n")
		}
		b.WriteString("\n")
		b.WriteString(tui.HelpStyle.Render("j/k: navigate . Enter: continue . Esc: cancel"))

	case stepConcept:
		b.WriteString("Title: " + m.titleInput.Value() + "\n")
		b.WriteString("Type: " + createTypeOptions[m.createTypeIdx] + "\n\n")
		b.WriteString("Select concept (optional):\n\n")
		b.WriteString(m.conceptPicker.View())
		b.WriteString("\n")
		b.WriteString(tui.HelpStyle.Render("Tab: skip . Esc: back"))
	}

	return b.String()
}
