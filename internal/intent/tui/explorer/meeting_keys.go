package explorer

import (
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

// isMeetingNote reports whether the intent is a structured meeting note.
func isMeetingNote(i *intent.Intent) bool {
	if i == nil {
		return false
	}
	if i.Status != intent.StatusNoteMeetings {
		return false
	}
	return i.Meeting != nil
}

// handleMeetingTranscript opens the transcript sidecar for the selected meeting.
func (m *Model) handleMeetingTranscript(selected *intent.Intent) (tea.Model, tea.Cmd) {
	if m.service == nil {
		m.statusMessage = "Transcript requires an intent service"
		return m, nil
	}
	path, ok := m.service.MeetingTranscriptPath(selected)
	if !ok {
		m.statusMessage = "No transcript sidecar for this meeting"
		return m, nil
	}
	return m, openInEditor(m.ctx, path)
}

// handleMeetingAudio opens the machine-local audio file referenced by the meeting.
func (m *Model) handleMeetingAudio(selected *intent.Intent) (tea.Model, tea.Cmd) {
	if selected.Meeting == nil || selected.Meeting.Bundle == "" || selected.Meeting.Audio == "" {
		m.statusMessage = "No audio path on this meeting"
		return m, nil
	}
	audioPath := filepath.Join(selected.Meeting.Bundle, selected.Meeting.Audio)
	return m, openWithSystem(audioPath)
}

// handleMeetingExtract creates inbox intents from checklist lines in the summary.
func (m *Model) handleMeetingExtract(selected *intent.Intent) (tea.Model, tea.Cmd) {
	if m.service == nil {
		m.statusMessage = "Extract requires an intent service"
		return m, nil
	}
	items := extractItemsFromMeetingBody(selected.Content)
	if len(items) == 0 {
		m.statusMessage = "No action items found in meeting summary"
		return m, nil
	}
	return m, m.runMeetingExtract(selected, items)
}

func (m *Model) runMeetingExtract(selected *intent.Intent, items []intent.ExtractItem) tea.Cmd {
	return func() tea.Msg {
		created, err := m.service.ExtractMeetingIntents(m.ctx, selected.ID, items)
		if err != nil {
			return folderFinishedMsg{err: err}
		}
		for _, it := range created {
			_ = m.appendAuditEvent(audit.Event{
				Type:   audit.EventCreate,
				ID:     it.ID,
				Title:  it.Title,
				To:     string(it.Status),
				Reason: "extracted from meeting " + selected.ID,
			})
			m.autoCommitIntent(commit.IntentCreate, it.Title, "Extracted from meeting "+selected.ID, it.Path)
		}
		msg := "Extracted 0 new intents (already present or empty)"
		if n := len(created); n > 0 {
			msg = "Extracted " + itoa(n) + " intent(s) from meeting"
		}
		return folderFinishedMsg{message: msg}
	}
}

var actionItemLine = regexp.MustCompile(`(?i)^\s*[-*]\s*\[[ xX]\]\s+(.+)$`)

// extractItemsFromMeetingBody pulls checkbox action items from the summary body.
func extractItemsFromMeetingBody(body string) []intent.ExtractItem {
	var items []intent.ExtractItem
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		m := actionItemLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		title := strings.TrimSpace(m[1])
		if title == "" || seen[strings.ToLower(title)] {
			continue
		}
		seen[strings.ToLower(title)] = true
		items = append(items, intent.ExtractItem{Title: title})
	}
	return items
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
