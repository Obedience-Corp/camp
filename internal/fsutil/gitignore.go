package fsutil

import (
	"os"
	"strings"
)

// Gitignore rule editing, shared so camp has exactly one writer of ignore
// rules. Scaffold owns the campaign's own state rules during init and repair;
// `camp artifacts add` owns the rule for a clean artifact root. Two
// independent append implementations would drift on the details that matter
// here: what counts as "already present", and whether a trailing newline is
// preserved.

// HasGitignoreRule reports whether content contains an active rule equal to
// entry.
//
// Lines are compared after trimming; blank lines and comments are skipped. A
// substring match such as `not-<entry>`, or a commented-out `# <entry>`, is
// deliberately not a match, because git would still track the file and
// reporting the rule as present would be wrong in the direction that loses
// data.
func HasGitignoreRule(content, entry string) bool {
	target := strings.TrimSpace(entry)
	if target == "" {
		return false
	}
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == target {
			return true
		}
	}
	return false
}

// AppendGitignoreEntryIfMissing adds entry to the gitignore file at path,
// preceded by comment, unless an active rule for it is already there. It
// reports whether it wrote anything, so callers can tell the user what
// actually happened rather than claiming an edit that did not occur.
//
// A missing file is created: "ensure this rule exists" is the caller's intent
// whether or not the file is there yet. The write is atomic, and a file that
// did not end in a newline gains a blank separator line so the appended rule
// never joins the last line.
func AppendGitignoreEntryIfMissing(path, entry, comment string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if HasGitignoreRule(string(raw), entry) {
		return false, nil
	}

	var addition strings.Builder
	if len(raw) > 0 {
		addition.WriteString("\n")
		if raw[len(raw)-1] != '\n' {
			addition.WriteString("\n")
		}
	}
	if comment != "" {
		addition.WriteString(comment)
		addition.WriteString("\n")
	}
	addition.WriteString(entry)
	addition.WriteString("\n")

	if err := WriteFileAtomically(path, append(raw, addition.String()...), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
