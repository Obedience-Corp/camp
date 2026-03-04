package explorer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/promote"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
)

// moveStatusOptions are the available statuses for moving intents.
// Dungeon statuses are visually indented to show hierarchy.
var moveStatusOptions = []struct {
	name   string
	status intent.Status
}{
	{"Inbox", intent.StatusInbox},
	{"Ready", intent.StatusReady},
	{"Active", intent.StatusActive},
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
			// Require reason for dungeon moves
			if newStatus.InDungeon() {
				m.dungeonReasonFor = newStatus
				m.dungeonReasonIntent = m.intentToMove
				m.intentToMove = nil
				m.focus = focusDungeonReason
				ti := textinput.New()
				ti.Placeholder = "Enter reason..."
				ti.CharLimit = 200
				ti.Width = 60
				ti.Focus()
				m.dungeonReasonInput = ti
				return m, nil
			}
			m.focus = focusList
			return m, m.moveIntent(m.intentToMove, newStatus)
		}
	}
	return m, nil
}

// moveIntent moves an intent to a new status (non-dungeon, no reason required).
func (m *Model) moveIntent(i *intent.Intent, newStatus intent.Status) tea.Cmd {
	return func() tea.Msg {
		sourcePath := i.Path
		prevStatus := i.Status
		movedIntent, err := m.service.Move(m.ctx, i.ID, newStatus)
		if err == nil {
			// Log audit event
			audit.AppendEvent(m.ctx, m.intentsDir, audit.Event{
				Type:  audit.EventMove,
				ID:    i.ID,
				Title: i.Title,
				From:  string(prevStatus),
				To:    string(newStatus),
			})

			if m.campaignRoot != "" && m.campaignID != "" {
				_ = commit.Intent(m.ctx, commit.IntentOptions{
					Options: commit.Options{
						CampaignRoot:  m.campaignRoot,
						CampaignID:    m.campaignID,
						Files:         commit.NormalizeFiles(m.campaignRoot, sourcePath, movedIntent.Path),
						SelectiveOnly: true,
					},
					Action:      commit.IntentMove,
					IntentTitle: i.Title,
					Description: fmt.Sprintf("Moved to %s status", newStatus),
				})
			}
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
		prevStatus := i.Status
		archivedIntent, err := m.service.Archive(m.ctx, i.ID)
		if err == nil {
			audit.AppendEvent(m.ctx, m.intentsDir, audit.Event{
				Type:  audit.EventArchive,
				ID:    i.ID,
				Title: i.Title,
				From:  string(prevStatus),
				To:    string(intent.StatusArchived),
			})

			if m.campaignRoot != "" && m.campaignID != "" {
				_ = commit.Intent(m.ctx, commit.IntentOptions{
					Options: commit.Options{
						CampaignRoot:  m.campaignRoot,
						CampaignID:    m.campaignID,
						Files:         commit.NormalizeFiles(m.campaignRoot, sourcePath, archivedIntent.Path),
						SelectiveOnly: true,
					},
					Action:      commit.IntentArchive,
					IntentTitle: i.Title,
				})
			}
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
		prevStatus := i.Status
		err := m.service.Delete(m.ctx, i.ID)
		if err == nil {
			audit.AppendEvent(m.ctx, m.intentsDir, audit.Event{
				Type:  audit.EventDelete,
				ID:    i.ID,
				Title: title,
				From:  string(prevStatus),
			})

			if m.campaignRoot != "" && m.campaignID != "" {
				_ = commit.Intent(m.ctx, commit.IntentOptions{
					Options: commit.Options{
						CampaignRoot:  m.campaignRoot,
						CampaignID:    m.campaignID,
						Files:         commit.NormalizeFiles(m.campaignRoot, sourcePath),
						SelectiveOnly: true,
					},
					Action:      commit.IntentDelete,
					IntentTitle: title,
				})
			}
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
			Target:       promote.TargetFestival,
		})

		if err == nil {
			promotedTo := result.FestivalDir
			audit.AppendEvent(m.ctx, m.intentsDir, audit.Event{
				Type:       audit.EventPromote,
				ID:         i.ID,
				Title:      i.Title,
				From:       string(i.Status),
				To:         string(intent.StatusActive),
				PromotedTo: promotedTo,
			})

			if m.campaignRoot != "" && m.campaignID != "" {
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
						CampaignRoot:  m.campaignRoot,
						CampaignID:    m.campaignID,
						Files:         commit.NormalizeFiles(m.campaignRoot, files...),
						SelectiveOnly: true,
					},
					Action:      commit.IntentPromote,
					IntentTitle: i.Title,
					Description: "Promoted to active via festival",
				})
			}
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

// handlePromoteAction shows the promote target picker.
// Valid for inbox (→ ready) and ready (→ festival or design doc) intents.
func (m *Model) handlePromoteAction() (tea.Model, tea.Cmd) {
	if selected := m.SelectedIntent(); selected != nil {
		targets := promote.ValidTargetsForStatus(selected.Status)
		if len(targets) == 0 {
			m.statusMessage = "No valid promote targets for " + selected.Status.String() + " status"
			return m, nil
		}
		// If only one target, go directly to confirmation
		if len(targets) == 1 && targets[0] == promote.TargetReady {
			m.focus = focusConfirm
			m.pendingAction = "promote-ready"
			m.pendingIntent = selected
			m.confirmDialog = tui.NewConfirmationDialog(
				"Promote to Ready",
				fmt.Sprintf("Move '%s' from inbox to ready?", selected.Title),
			)
			return m, nil
		}
		m.focus = focusPromoteTarget
		m.promoteTargetIdx = 0
		m.promoteTargetIntent = selected
	}
	return m, nil
}

// promoteTargetOptions returns display names for promote targets.
var promoteTargetOptions = []struct {
	name   string
	target promote.Target
}{
	{"Festival", promote.TargetFestival},
	{"Design Doc", promote.TargetDesign},
}

// updatePromoteTarget handles key input during promote target selection.
func (m *Model) updatePromoteTarget(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusList
		m.promoteTargetIntent = nil
		return m, nil
	case "j", "down":
		if m.promoteTargetIdx < len(promoteTargetOptions)-1 {
			m.promoteTargetIdx++
		}
	case "k", "up":
		if m.promoteTargetIdx > 0 {
			m.promoteTargetIdx--
		}
	case "enter":
		if m.promoteTargetIntent != nil {
			target := promoteTargetOptions[m.promoteTargetIdx].target
			i := m.promoteTargetIntent
			m.promoteTargetIntent = nil
			m.focus = focusList

			switch target {
			case promote.TargetFestival:
				return m, m.promoteToFestival(i)
			case promote.TargetDesign:
				return m, m.promoteToDesignDoc(i)
			}
		}
	}
	return m, nil
}

// viewPromoteTarget renders the promote target picker.
func (m *Model) viewPromoteTarget() string {
	var b strings.Builder

	b.WriteString(tui.TitleStyle.Render("Promote Intent"))
	b.WriteString("\n\n")

	if m.promoteTargetIntent != nil {
		b.WriteString("Intent: " + m.promoteTargetIntent.Title + "\n")
		b.WriteString("Current status: " + m.promoteTargetIntent.Status.String() + "\n\n")
	}

	b.WriteString("Select promote target:\n")
	for i, opt := range promoteTargetOptions {
		cursor := "  "
		if i == m.promoteTargetIdx {
			cursor = "> "
		}
		b.WriteString(cursor + opt.name + "\n")
	}
	b.WriteString("\n")
	b.WriteString(tui.HelpStyle.Render("j/k: navigate . Enter: promote . Esc: cancel"))

	return b.String()
}

// promoteToReady promotes an inbox intent to ready status.
func (m *Model) promoteToReady(i *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		result, err := promote.Promote(m.ctx, m.service, i, promote.Options{
			CampaignRoot: m.campaignRoot,
			Target:       promote.TargetReady,
		})

		if err == nil && m.campaignRoot != "" && m.campaignID != "" {
			movedIntent, findErr := m.service.Get(m.ctx, i.ID)
			var files []string
			if findErr == nil && movedIntent.Path != "" {
				files = []string{i.Path, movedIntent.Path}
			} else {
				files = []string{i.Path}
			}

			_ = commit.Intent(m.ctx, commit.IntentOptions{
				Options: commit.Options{
					CampaignRoot:  m.campaignRoot,
					CampaignID:    m.campaignID,
					Files:         commit.NormalizeFiles(m.campaignRoot, files...),
					SelectiveOnly: true,
				},
				Action:      commit.IntentPromote,
				IntentTitle: i.Title,
				Description: "Promoted to ready",
			})
		}

		_ = result // no festival/design info for ready promotion
		return moveFinishedMsg{
			err:       err,
			intentID:  i.ID,
			newStatus: intent.StatusReady,
		}
	}
}

// promoteToDesignDoc promotes an intent to a design doc.
func (m *Model) promoteToDesignDoc(i *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		result, err := promote.Promote(m.ctx, m.service, i, promote.Options{
			CampaignRoot: m.campaignRoot,
			Target:       promote.TargetDesign,
		})

		if err == nil && m.campaignRoot != "" && m.campaignID != "" {
			files := []string{i.Path}
			movedIntent, findErr := m.service.Get(m.ctx, i.ID)
			if findErr == nil && movedIntent.Path != "" {
				files = append(files, movedIntent.Path)
			}
			if result.DesignCreated && result.DesignDir != "" {
				files = append(files, result.DesignDir)
			}

			_ = commit.Intent(m.ctx, commit.IntentOptions{
				Options: commit.Options{
					CampaignRoot:  m.campaignRoot,
					CampaignID:    m.campaignID,
					Files:         commit.NormalizeFiles(m.campaignRoot, files...),
					SelectiveOnly: true,
				},
				Action:      commit.IntentPromote,
				IntentTitle: i.Title,
				Description: "Promoted to design doc",
			})
		}

		return promoteFinishedMsg{
			err:         err,
			intentID:    i.ID,
			intentTitle: i.Title,
			designDir:   result.DesignDir,
		}
	}
}

// updateDungeonReason handles key input for the dungeon reason text input.
func (m *Model) updateDungeonReason(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusList
		m.dungeonReasonIntent = nil
		return m, nil
	case "enter":
		reason := m.dungeonReasonInput.Value()
		if reason == "" {
			m.statusMessage = "Reason is required for dungeon moves"
			return m, nil
		}
		i := m.dungeonReasonIntent
		newStatus := m.dungeonReasonFor
		m.dungeonReasonIntent = nil
		m.focus = focusList

		// Append decision record and move
		return m, m.moveIntentWithReason(i, newStatus, reason)
	default:
		var cmd tea.Cmd
		m.dungeonReasonInput, cmd = m.dungeonReasonInput.Update(msg)
		return m, cmd
	}
}

// viewDungeonReason renders the dungeon reason text input.
func (m *Model) viewDungeonReason() string {
	var b strings.Builder

	b.WriteString(tui.TitleStyle.Render("Dungeon Move — Reason Required"))
	b.WriteString("\n\n")

	if m.dungeonReasonIntent != nil {
		b.WriteString("Intent: " + m.dungeonReasonIntent.Title + "\n")
		b.WriteString("Moving to: " + m.dungeonReasonFor.String() + "\n\n")
	}

	b.WriteString("Why is this intent being moved to the dungeon?\n\n")
	b.WriteString(m.dungeonReasonInput.View())
	b.WriteString("\n\n")
	b.WriteString(tui.HelpStyle.Render("Enter: confirm . Esc: cancel"))

	return b.String()
}

// moveIntentWithReason moves an intent to a dungeon status with a decision record.
func (m *Model) moveIntentWithReason(i *intent.Intent, newStatus intent.Status, reason string) tea.Cmd {
	return func() tea.Msg {
		sourcePath := i.Path

		// Append decision record before moving
		intent.AppendDecisionRecord(i, newStatus, reason)
		_ = m.service.Save(m.ctx, i)

		movedIntent, err := m.service.Move(m.ctx, i.ID, newStatus)
		if err == nil && m.campaignRoot != "" && m.campaignID != "" {
			// Log audit event
			audit.AppendEvent(m.ctx, m.intentsDir, audit.Event{
				Type:   audit.EventMove,
				ID:     i.ID,
				Title:  i.Title,
				From:   string(i.Status),
				To:     string(newStatus),
				Reason: reason,
			})

			_ = commit.Intent(m.ctx, commit.IntentOptions{
				Options: commit.Options{
					CampaignRoot:  m.campaignRoot,
					CampaignID:    m.campaignID,
					Files:         commit.NormalizeFiles(m.campaignRoot, sourcePath, movedIntent.Path),
					SelectiveOnly: true,
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
