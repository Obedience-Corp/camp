package shell

import (
	"bytes"
	"embed"
	"strings"
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

// rootBoolFlags are camp's root persistent flags. One may come before or
// between the command words, as in `camp --no-color dungeon explore`, so a
// wrapper arm that must find its command reads past them. Every one is
// boolean: a flag that took a value would need that value skipped as well.
var rootBoolFlags = []string{"no-color"}

// RootFlagCase is the root flags as a POSIX case pattern: each flag bare and
// with a value attached.
func (templateData) RootFlagCase() string {
	patterns := make([]string, 0, 2*len(rootBoolFlags))
	for _, name := range rootBoolFlags {
		patterns = append(patterns, "--"+name, "--"+name+"=*")
	}
	return strings.Join(patterns, "|")
}

// RootFlagFish is the root flags as fish case patterns.
func (templateData) RootFlagFish() string {
	patterns := make([]string, 0, 2*len(rootBoolFlags))
	for _, name := range rootBoolFlags {
		patterns = append(patterns, "--"+name, "'--"+name+"=*'")
	}
	return strings.Join(patterns, " ")
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
