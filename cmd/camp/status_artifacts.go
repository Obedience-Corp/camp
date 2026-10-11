package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// Keep generated arguments comfortably below supported platforms' exec limits.
// Beyond this budget the CLI reports plain git status with an explicit notice.
const statusExclusionBudget = 16 * 1024

func withArtifactExclusions(gitArgs, paths []string) ([]string, bool) {
	size := 0
	for _, p := range paths {
		size += len(p) + len(":(exclude,literal)") + 1
		if size > statusExclusionBudget {
			return gitArgs, false
		}
	}
	if len(paths) == 0 {
		return gitArgs, true
	}
	args := append([]string{}, gitArgs...)
	if !containsDashDash(args) {
		args = append(args, "--")
	}
	for _, p := range paths {
		args = append(args, ":(exclude,literal)"+p)
	}
	return args, true
}

func containsDashDash(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return true
		}
	}
	return false
}

// Git options must precede an explicit pathspec separator.
func withStatusOptions(args []string, options ...string) []string {
	split := len(args)
	for i, arg := range args {
		if arg == "--" {
			split = i
			break
		}
	}
	result := append([]string{}, args[:split]...)
	result = append(result, options...)
	return append(result, args[split:]...)
}

// Ask Git to apply the user's pathspecs, including glob and exclude magic.
// Only untracked paths selected by that same request belong in our section.
//
// A request with no pathspec already selected every candidate. UntrackedContent
// found them with a status limited to the declared roots. Running a second
// status of the whole camp here only repeats the scan the printed status is
// about to do.
func scopedStatusArtifacts(ctx context.Context, repo string, args []string, roots []artifacts.UntrackedRoot) ([]artifacts.UntrackedRoot, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	if !statusHasPathspec(args) {
		return roots, nil
	}
	scopeArgs := withStatusOptions(args, "--porcelain=v1", "-z", "--untracked-files=all")
	output, err := git.StatusPorcelain(ctx, repo, scopeArgs...)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]bool)
	for _, entry := range git.ParseStatusPorcelainZ(output) {
		if entry.Code == "??" {
			selected[entry.Path] = true
		}
	}
	var scoped []artifacts.UntrackedRoot
	for _, root := range roots {
		group := artifacts.UntrackedRoot{Root: root.Root}
		for _, file := range root.Files {
			if selected[file.Path] {
				group.Files = append(group.Files, file)
				group.Bytes += file.Size
			}
		}
		if len(group.Files) > 0 {
			scoped = append(scoped, group)
		}
	}
	return scoped, nil
}

// statusHasPathspec reports whether args limit the status to paths.
//
// Flags before the separator are options, including the ones camp adds
// (--ignore-submodules=all, --short). Anything after the separator is a
// pathspec, even when it looks like a flag. A non-option before the
// separator is a pathspec too.
func statusHasPathspec(args []string) bool {
	afterSeparator := false
	for _, arg := range args {
		if afterSeparator {
			return true
		}
		if arg == "--" {
			afterSeparator = true
			continue
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
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
	count := 0
	var size int64
	for _, root := range roots {
		count += len(root.Files)
		size += root.Bytes
	}
	if count == 0 {
		return
	}
	noun := "artifacts"
	if count == 1 {
		noun = "artifact"
	}
	summary := fmt.Sprintf("%s %s · %s kept out of git", ui.FormatCount(count), noun, ui.FormatBytes(size))
	if format == statusFormatLong {
		_, _ = fmt.Fprintf(out, "\n%s  %s\n", ui.Accent(summary), ui.Dim("→ camp artifacts"))
	} else {
		_, _ = fmt.Fprintf(out, "%s → camp artifacts\n", summary)
	}
}
