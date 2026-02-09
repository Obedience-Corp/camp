package tui

import (
	"fmt"
	"time"

	"github.com/obediencecorp/camp/internal/intent"
)

// MoveStatusOptions are the available statuses for moving intents.
var moveStatusOptions = []struct {
	name   string
	status intent.Status
}{
	{"Inbox", intent.StatusInbox},
	{"Active", intent.StatusActive},
	{"Ready", intent.StatusReady},
	{"Done", intent.StatusDone},
	{"Killed", intent.StatusKilled},
	{"Archived", intent.StatusArchived},
	{"Someday", intent.StatusSomeday},
}

// StatusWorkflow defines the promotion order for intents.
// Dungeon statuses are excluded — promotion ends at done.
var statusWorkflow = []intent.Status{
	intent.StatusInbox,
	intent.StatusActive,
	intent.StatusReady,
	intent.StatusDone,
}

// getNextStatus returns the next status in the promotion workflow.
// Returns the same status if already at the final state.
func getNextStatus(current intent.Status) intent.Status {
	for i, s := range statusWorkflow {
		if s == current && i < len(statusWorkflow)-1 {
			return statusWorkflow[i+1]
		}
	}
	return current // No change if at end or not in workflow
}

// formatRelativeTime returns a human-friendly relative time string.
func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("Jan 2")
	}
}
