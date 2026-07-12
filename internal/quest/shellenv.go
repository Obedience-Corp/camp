package quest

import (
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// QuestEnvVar carries terminal-local quest context. A child process inherits it
// so commands such as `camp workitem create` can stamp the active quest without
// a campaign-global "active quest" file that every terminal would fight over.
const QuestEnvVar = "CAMP_QUEST"

// ShellDialect selects the syntax used to render environment mutations.
type ShellDialect string

const (
	// ShellPosix covers bash, zsh, and POSIX sh (export/unset).
	ShellPosix ShellDialect = "posix"
	// ShellFish covers fish (set -gx / set -e).
	ShellFish ShellDialect = "fish"
)

// ParseShellDialect maps a shell name to a dialect. Empty defaults to POSIX.
func ParseShellDialect(raw string) (ShellDialect, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "posix", "sh", "bash", "zsh":
		return ShellPosix, nil
	case "fish":
		return ShellFish, nil
	default:
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "unsupported shell %q (use posix, bash, zsh, or fish)", raw)
	}
}

// RenderActivate returns the shell code that sets QuestEnvVar to questID. The
// value is quoted so it is safe to eval; callers must validate the quest before
// emitting so stdout stays empty on failure.
func RenderActivate(dialect ShellDialect, questID string) string {
	switch dialect {
	case ShellFish:
		return fmt.Sprintf("set -gx %s %s", QuestEnvVar, fishQuote(questID))
	default:
		return fmt.Sprintf("export %s=%s", QuestEnvVar, posixQuote(questID))
	}
}

// RenderClear returns the shell code that removes QuestEnvVar.
func RenderClear(dialect ShellDialect) string {
	switch dialect {
	case ShellFish:
		return fmt.Sprintf("set -e %s", QuestEnvVar)
	default:
		return fmt.Sprintf("unset %s", QuestEnvVar)
	}
}

// posixQuote single-quotes a value for bash/zsh/sh, escaping embedded quotes.
func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// fishQuote single-quotes a value for fish, escaping backslashes then quotes.
func fishQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", `\'`)
	return "'" + s + "'"
}
