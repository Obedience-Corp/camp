package shell

import (
	"bytes"
	"embed"
	"text/template"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// templateData holds values substituted into shell init templates.
type templateData struct {
	// ShortcutWords is a space-separated list of shortcut keys (bash compgen -W).
	ShortcutWords string

	// ShortcutTargets is zsh _describe target array entries, one per line.
	ShortcutTargets string

	// ShortcutCompletions is fish complete commands for cgo shortcuts.
	ShortcutCompletions string

	// ShellWords is the supported shells for bash compgen -W, space separated.
	ShellWords string

	// ShellQuoted is the supported shells as a zsh array literal.
	ShellQuoted string

	// ShellCompletions is fish complete commands for the shell-init argument.
	ShellCompletions string
}

// parsedTemplates caches the parsed template tree so we parse once per process.
var parsedTemplates = template.Must(
	template.New("").ParseFS(templateFS, "templates/*.tmpl"),
)

// renderTemplate executes a named template with the given data and returns the output.
func renderTemplate(name string, data templateData) (string, error) {
	var buf bytes.Buffer
	if err := parsedTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", camperrors.Wrapf(err, "render template %s", name)
	}
	return buf.String(), nil
}
