package intent

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func parseIntentStatus(raw string) (intent.Status, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "inbox":
		return intent.StatusInbox, nil
	case "ready":
		return intent.StatusReady, nil
	case "active":
		return intent.StatusActive, nil
	case "done", string(intent.StatusDone):
		return intent.StatusDone, nil
	case "killed", string(intent.StatusKilled):
		return intent.StatusKilled, nil
	case "archived", string(intent.StatusArchived):
		return intent.StatusArchived, nil
	case "someday", string(intent.StatusSomeday):
		return intent.StatusSomeday, nil
	default:
		return "", fmt.Errorf("invalid status: %s (use inbox, ready, active, done, killed, archived, someday, or dungeon/<status>)", raw)
	}
}
