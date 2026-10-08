package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// statusArtifactListLimit is how many artifact files the long format names
// individually before it falls back to one line per root.
const statusArtifactListLimit = 10

// withArtifactExclusions appends exclude pathspecs that keep artifact content
// out of git's untracked listing. They go last, after every flag camp adds,
// and reuse a "--" the user already passed so a user pathspec still applies.
func withArtifactExclusions(gitArgs, paths []string) []string {
	if len(paths) == 0 {
		return gitArgs
	}
	args := append([]string{}, gitArgs...)
	if !containsDashDash(args) {
		args = append(args, "--")
	}
	for _, p := range paths {
		args = append(args, ":(exclude,literal)"+p)
	}
	return args
}

func containsDashDash(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return true
		}
	}
	return false
}

// statusFormat is the git status output form a camp status call asked for.
type statusFormat int

const (
	statusFormatLong statusFormat = iota
	statusFormatShort
	// statusFormatMachine is porcelain or NUL-terminated output, which scripts
	// parse; nothing camp adds may land on its stdout.
	statusFormatMachine
)

// detectStatusFormat reads the forwarded git flags. Arguments after "--" are
// pathspecs and never flags.
func detectStatusFormat(gitArgs []string) statusFormat {
	format := statusFormatLong
	for _, a := range gitArgs {
		switch {
		case a == "--":
			return format
		case a == "--porcelain" || strings.HasPrefix(a, "--porcelain=") || a == "--null":
			return statusFormatMachine
		case a == "--short":
			format = statusFormatShort
		case a == "--long":
			format = statusFormatLong
		case strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--"):
			if strings.Contains(a, "z") {
				return statusFormatMachine
			}
			if strings.Contains(a, "s") {
				format = statusFormatShort
			}
		}
	}
	return format
}

// renderStatusArtifacts reports the untracked files camp keeps out of git as
// artifact content, so leaving them out of git's listing never hides them.
func renderStatusArtifacts(out io.Writer, roots []artifacts.UntrackedRoot, format statusFormat) {
	if len(roots) == 0 {
		return
	}
	if format != statusFormatLong {
		for _, r := range roots {
			_, _ = fmt.Fprintf(out, "artifact content kept out of git: %s/ (%s)\n", r.Root, fileTally(r))
		}
		return
	}

	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Artifact content:")
	_, _ = fmt.Fprintln(out, `  (kept out of git; "camp sync --from <machine>" copies it between machines)`)
	if len(artifacts.UntrackedPaths(roots)) <= statusArtifactListLimit {
		for _, r := range roots {
			for _, f := range r.Files {
				_, _ = fmt.Fprintf(out, "\t%s (%s)\n", f.Path, ui.FormatBytes(f.Size))
			}
		}
		return
	}
	for _, r := range roots {
		_, _ = fmt.Fprintf(out, "\t%s/ (%s)\n", r.Root, fileTally(r))
	}
}

func fileTally(r artifacts.UntrackedRoot) string {
	noun := "files"
	if len(r.Files) == 1 {
		noun = "file"
	}
	return fmt.Sprintf("%s %s, %s", ui.FormatCount(len(r.Files)), noun, ui.FormatBytes(r.Bytes))
}
