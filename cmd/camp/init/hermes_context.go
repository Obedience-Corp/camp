package initcmd

import (
	"context"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/Obedience-Corp/camp/internal/ui"
)

// Hermes loads exactly one project-context file per session; first match
// wins. These names outrank AGENTS.md, so a sibling silently replaces the
// campaign's agent instructions instead of overlaying them.
var hermesContextOverrideNames = []string{
	".hermes.md",
	"HERMES.md",
	"AGENTS.override.md",
}

const hermesContextDocsURL = "https://docs.fest.build/getting-started/agents/hermes/"

func listHermesContextOverrides(ctx context.Context, fsys fs.FS) []string {
	if ctx.Err() != nil {
		return nil
	}
	var found []string
	for _, name := range hermesContextOverrideNames {
		if ctx.Err() != nil {
			return found
		}
		info, err := fs.Stat(fsys, name)
		if err != nil || info.IsDir() {
			continue
		}
		found = append(found, name)
	}
	return found
}

func formatHermesContextWarning(names []string) string {
	if len(names) == 0 {
		return ""
	}
	verb := "sits"
	replacer := "that file replaces"
	if len(names) > 1 {
		verb = "sit"
		replacer = "those files replace"
	}
	return "warning: " + joinAnd(names) + " " + verb +
		" next to AGENTS.md; Hermes loads exactly one project-context file per session (first match wins), so " +
		replacer + " AGENTS.md instead of overlaying it. " + hermesContextDocsURL
}

func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
	}
}

func emitHermesContextWarning(ctx context.Context, campaignRoot string, w Writers) {
	if campaignRoot == "" {
		return
	}
	emitHermesContextWarningFS(ctx, os.DirFS(campaignRoot), w)
}

func emitHermesContextWarningFS(ctx context.Context, fsys fs.FS, w Writers) {
	msg := formatHermesContextWarning(listHermesContextOverrides(ctx, fsys))
	if msg == "" {
		return
	}
	out := w.ErrOut
	if out == nil {
		out = os.Stderr
	}
	writeHermesWarning(out, msg)
}

func writeHermesWarning(w io.Writer, msg string) {
	writeLine(w, ui.WarningIcon(), ui.Warning(msg))
}
