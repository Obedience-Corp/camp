package fresh

import (
	"fmt"
	"io"
	"os"

	"github.com/Obedience-Corp/camp/internal/ui"
)

func freshRow(label, status string, tone ui.StatusTone) {
	_ = ui.WriteChecklistRow(os.Stdout, ui.TermColumns(), ui.ChecklistRowIndent, ui.ChecklistMark(tone), label, status, tone)
}

func freshHeading(title, note string) {
	_ = ui.WriteChecklistSection(os.Stdout, ui.TermColumns(), title, note)
}

func freshDetail(text string) {
	writeFreshDetail(os.Stdout, ui.TermColumns(), text)
}

func writeFreshDetail(w io.Writer, width int, text string) {
	_ = ui.WriteChecklistDetail(w, width, ui.ChecklistDetailIndent, text)
}

// pruneChecklistStatus is the short result for a prune step. Branch names are
// returned separately so the status column stays a count.
func pruneChecklistStatus(deleted []string, worktrees int, dryRun bool) (status string, names []string) {
	branchVerb := "deleted"
	treeVerb := "removed"
	if dryRun {
		branchVerb = "would delete"
		treeVerb = "would remove"
	}
	switch {
	case len(deleted) == 0 && worktrees > 0:
		status = treeVerb + " " + countNoun(worktrees, "worktree", "worktrees")
	case len(deleted) > 0 && worktrees == 0:
		status = branchVerb + " " + countNoun(len(deleted), "branch", "branches")
		names = deleted
	case len(deleted) > 0:
		status = fmt.Sprintf("%s %s, %s %s",
			branchVerb, countNoun(len(deleted), "branch", "branches"),
			treeVerb, countNoun(worktrees, "worktree", "worktrees"))
		names = deleted
	}
	return status, names
}

func countNoun(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
