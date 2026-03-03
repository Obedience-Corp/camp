package explorer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/promote"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// moveStatusOptions are the available statuses for moving intents.
// Dungeon statuses are visually indented to show hierarchy.
var moveStatusOptions = []struct {
	name   string
	status intent.Status
}{
	{"Inbox", intent.StatusInbox},
	{"Active", intent.StatusActive},
	{"Ready", intent.StatusReady},
	{"  Done", intent.StatusDone},
	{"  Killed", intent.StatusKilled},
	{"  Archived", intent.StatusArchived},
	{"  Someday", intent.StatusSomeday},
}

// updateMove handles key input during move action.
func (m *Model) updateMove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel move
		m.focus = focusList
		m.intentToMove = nil
		return m, nil
	case "j", "down":
		if m.moveStatusIdx < len(moveStatusOptions)-1 {
			m.moveStatusIdx++
		}
	case "k", "up":
		if m.moveStatusIdx > 0 {
			m.moveStatusIdx--
		}
	case "enter":
		// Execute move
		if m.intentToMove != nil {
			newStatus := moveStatusOptions[m.moveStatusIdx].status
			if m.intentToMove.Status == newStatus {
				// Already at this status
				m.statusMessage = "Already at " + newStatus.String()
				m.focus = focusList
				m.intentToMove = nil
				return m, nil
			}
			m.focus = focusList
			return m, m.moveIntent(m.intentToMove, newStatus)
		}
	}
	return m, nil
}

// moveIntent moves an intent to a new status.
func (m *Model) moveIntent(i *intent.Intent, newStatus intent.Status) tea.Cmd {
	return func() tea.Msg {
		sourcePath := i.Path
		movedIntent, err := m.service.Move(m.ctx, i.ID, newStatus)
		if err == nil && m.campaignRoot != "" && m.campaignID != "" {
			_ = commit.Intent(m.ctx, commit.IntentOptions{
				Options: commit.Options{
					CampaignRoot: m.campaignRoot,
					CampaignID:   m.campaignID,
					Files:        commit.NormalizeFiles(m.campaignRoot, sourcePath, movedIntent.Path),
				},
				Action:      commit.IntentMove,
				IntentTitle: i.Title,
				Description: fmt.Sprintf("Moved to %s status", newStatus),
			})
		}
		return moveFinishedMsg{
			err:       err,
			intentID:  i.ID,
			newStatus: newStatus,
		}
	}
}

// archiveIntent archives an intent (moves to dungeon/archived status).
func (m *Model) archiveIntent(i *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		sourcePath := i.Path
		archivedIntent, err := m.service.Archive(m.ctx, i.ID)
		if err == nil && m.campaignRoot != "" && m.campaignID != "" {
			_ = commit.Intent(m.ctx, commit.IntentOptions{
				Options: commit.Options{
					CampaignRoot: m.campaignRoot,
					CampaignID:   m.campaignID,
					Files:        commit.NormalizeFiles(m.campaignRoot, sourcePath, archivedIntent.Path),
				},
				Action:      commit.IntentArchive,
				IntentTitle: i.Title,
			})
		}
		return archiveFinishedMsg{
			err:      err,
			intentID: i.ID,
		}
	}
}

// deleteIntent permanently deletes an intent.
func (m *Model) deleteIntent(i *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		title := i.Title // Capture before deletion
		sourcePath := i.Path
		err := m.service.Delete(m.ctx, i.ID)
		if err == nil && m.campaignRoot != "" && m.campaignID != "" {
			_ = commit.Intent(m.ctx, commit.IntentOptions{
				Options: commit.Options{
					CampaignRoot: m.campaignRoot,
					CampaignID:   m.campaignID,
					Files:        commit.NormalizeFiles(m.campaignRoot, sourcePath),
				},
				Action:      commit.IntentDelete,
				IntentTitle: title,
			})
		}
		return deleteFinishedMsg{
			err:   err,
			title: title,
		}
	}
}

// promoteToFestival promotes an intent to a festival via the promote package.
func (m *Model) promoteToFestival(i *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		result, err := promote.Promote(m.ctx, m.service, i, promote.Options{
			CampaignRoot: m.campaignRoot,
		})

		if err == nil && m.campaignRoot != "" && m.campaignID != "" {
			files := []string{i.Path}
			movedIntent, findErr := m.service.Get(m.ctx, i.ID)
			if findErr == nil && movedIntent.Path != "" {
				files = append(files, movedIntent.Path)
			}
			if result.FestivalCreated && result.FestivalDest != "" && result.FestivalDir != "" {
				files = append(files, filepath.Join("festivals", result.FestivalDest, result.FestivalDir))
			}

			_ = commit.Intent(m.ctx, commit.IntentOptions{
				Options: commit.Options{
					CampaignRoot: m.campaignRoot,
					CampaignID:   m.campaignID,
					Files:        commit.NormalizeFiles(m.campaignRoot, files...),
				},
				Action:      commit.IntentPromote,
				IntentTitle: i.Title,
				Description: "Promoted to festival",
			})
		}

		return promoteFinishedMsg{
			err:             err,
			intentID:        i.ID,
			intentTitle:     i.Title,
			festNotFound:    result.FestNotFound,
			festivalCreated: result.FestivalCreated,
			festivalName:    result.FestivalName,
			festivalDir:     result.FestivalDir,
		}
	}
}

// viewMove renders the move status picker.
func (m *Model) viewMove() string {
	var b strings.Builder

	b.WriteString(tui.TitleStyle.Render("Move Intent"))
	b.WriteString("\n\n")

	if m.intentToMove != nil {
		b.WriteString("Moving: " + m.intentToMove.Title + "\n")
		b.WriteString("Current status: " + m.intentToMove.Status.String() + "\n\n")
	}

	b.WriteString("Select new status:\n")
	dungeonLabelShown := false
	for i, opt := range moveStatusOptions {
		// Show dungeon label before first dungeon status
		if !dungeonLabelShown && opt.status.InDungeon() {
			dungeonLabelShown = true
			b.WriteString(tui.HelpStyle.Render("  ── Dungeon ──") + "\n")
		}
		cursor := "  "
		if i == m.moveStatusIdx {
			cursor = "> "
		}
		// Mark current status
		marker := ""
		if m.intentToMove != nil && m.intentToMove.Status == opt.status {
			marker = " (current)"
		}
		b.WriteString(cursor + opt.name + marker + "\n")
	}
	b.WriteString("\n")
	b.WriteString(tui.HelpStyle.Render("j/k: navigate . Enter: move . Esc: cancel"))

	return b.String()
}

// viewConfirmation renders the confirmation dialog.
func (m *Model) viewConfirmation() string {
	var b strings.Builder

	b.WriteString(tui.TitleStyle.Render("Confirm Action"))
	b.WriteString("\n\n")
	b.WriteString(m.confirmDialog.View())

	return b.String()
}

// handlePromoteAction shows a confirmation dialog for promoting to festival.
// Only ready intents can be promoted.
func (m *Model) handlePromoteAction() (tea.Model, tea.Cmd) {
	if selected := m.SelectedIntent(); selected != nil {
		if selected.Status != intent.StatusReady {
			m.statusMessage = "Only ready intents can be promoted to festivals"
			return m, nil
		}
		m.focus = focusConfirm
		m.pendingAction = "promote"
		m.pendingIntent = selected
		m.confirmDialog = tui.NewConfirmationDialog(
			"Promote to Festival",
			fmt.Sprintf("Promote '%s' to a festival?\n\nThis will move the intent to done and create a new festival.", selected.Title),
		)
	}
	return m, nil
}

// handleArchiveAction archives the selected intent with confirmation.
func (m *Model) handleArchiveAction() (tea.Model, tea.Cmd) {
	if selected := m.SelectedIntent(); selected != nil {
		if selected.Status.InDungeon() {
			m.statusMessage = "Already in dungeon"
			return m, nil
		}
		m.focus = focusConfirm
		m.pendingAction = "archive"
		m.pendingIntent = selected
		m.confirmDialog = tui.NewConfirmationDialog(
			"Archive Intent",
			fmt.Sprintf("Archive '%s'?\n\nIt will be moved to the dungeon.", selected.Title),
		)
	}
	return m, nil
}

// handleDeleteAction deletes the selected intent with confirmation.
func (m *Model) handleDeleteAction() (tea.Model, tea.Cmd) {
	if selected := m.SelectedIntent(); selected != nil {
		m.focus = focusConfirm
		m.pendingAction = "delete"
		m.pendingIntent = selected
		m.confirmDialog = tui.NewConfirmationDialog(
			"Delete Intent",
			fmt.Sprintf("Delete '%s'?\n\nThis cannot be undone.", selected.Title),
		)
	}
	return m, nil
}
