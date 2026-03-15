package cmdutil

import (
	"errors"
	"strings"

	"github.com/Obedience-Corp/camp/internal/nav/index"
)

// FormatSubShortcutError formats an InvalidSubShortcutError for user display.
func FormatSubShortcutError(err *index.InvalidSubShortcutError) error {
	var msg strings.Builder
	msg.WriteString("Error: Unknown shortcut '")
	msg.WriteString(err.SubShortcut)
	msg.WriteString("' for project '")
	msg.WriteString(err.ProjectName)
	msg.WriteString("'\n")

	if len(err.AvailableNames) > 0 {
		msg.WriteString("Available shortcuts: ")
		msg.WriteString(strings.Join(err.AvailableNames, ", "))
		msg.WriteString("\n")
	} else {
		msg.WriteString("No shortcuts configured for this project.\n")
	}

	msg.WriteString("\nSee: camp shortcuts --help")
	return errors.New(msg.String())
}
