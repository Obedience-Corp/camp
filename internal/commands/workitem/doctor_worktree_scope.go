package workitem

import (
	"strconv"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// Doctor's worktree-scope findings. They live here rather than in doctor.go
// because that file and its two collector functions are already over the size
// limits, and adding to them makes the next reader's job worse.

// worktreeProjectFinding reports a worktree row that does not record the project
// it belongs to, when the path names one. Reading already resolves through the
// path; recording it is what makes the relationship outlive the path.
// Returns (finding, true) when there is something to report.
func worktreeProjectFinding(layout links.ScopeLayout, link links.Link) (docFinding, bool) {
	if link.Scope.Kind != links.ScopeWorktree || link.Scope.Project != "" {
		return docFinding{}, false
	}
	project := layout.WorktreeProject(link.Scope.Path)
	if project == "" {
		return docFinding{}, false
	}
	return docFinding{
		Code:        codeWorktreeProjectUnknown,
		Severity:    docSeverityInfo,
		Target:      "link:" + link.ID,
		Message:     "worktree scope " + link.Scope.Path + " does not record its project (" + project + ")",
		FixHint:     "run `camp workitem doctor --fix` to record it so the link survives the worktree",
		AutoFixable: true,
	}, true
}

// backfillScopeProject records the owning project on the worktree row a finding
// names. Returns whether it changed anything.
func backfillScopeProject(layout links.ScopeLayout, registry *links.Links, linkID string) bool {
	link, ok := registry.FindByID(linkID)
	if !ok {
		return false
	}
	project := layout.WorktreeProject(link.Scope.Path)
	if project == "" {
		return false
	}
	link.Scope.Project = project
	return true
}

// machineLocalFinding describes a scope target that is absent here but not gone
// everywhere. links.yaml is tracked, so the row must survive either way; the
// finding differs only in what it tells the reader.
//
// A worktree is deleted as a matter of course once its branch merges, and the
// workitem's real subject is the project the worktree checked out. When that
// project is still present, the missing directory is the expected end of a
// worktree's life rather than a problem, so it is reported as information.
// Everything else keeps the "not on this machine" warning, which is the honest
// answer for an uncloned submodule or a worktree camp cannot place.
func machineLocalFinding(root string, layout links.ScopeLayout, link links.Link) docFinding {
	if link.Scope.Kind == links.ScopeWorktree {
		if project := link.Scope.ProjectFor(layout); project != "" && scopeTargetExists(root, project) {
			return docFinding{
				Code:     codeWorktreeGone,
				Severity: docSeverityInfo,
				Target:   "link:" + link.ID,
				Message: "worktree " + link.Scope.Path + " is gone; workitem " + link.WorkitemID +
					" stays linked to " + project,
			}
		}
	}
	return docFinding{
		Code:     codeScopeNotLocal,
		Severity: docSeverityWarning,
		Target:   "link:" + link.ID,
		Message: "scope path " + link.Scope.Path + " is not on this machine" +
			" (" + string(link.Scope.Kind) + " scopes are machine-local)",
		FixHint: "expected if the worktree or submodule lives on another machine;" +
			" remove it explicitly with `camp workitem unlink --id " + link.ID + "` if it is really gone",
	}
}

// collapseWorktreeGone replaces the per-link worktree-gone findings with one
// summary row.
//
// This finding can never be cleared: a worktree deleted after its branch merged
// produces it on every run, and its own advice is that no action is needed. One
// line per link would put a permanent, growing block of text on a health command
// people are supposed to read, which is how they learn to stop reading it. The
// count still tells them the state, and `camp workitem links` names the rows.
func collapseWorktreeGone(findings []docFinding) []docFinding {
	var kept []docFinding
	count := 0
	for _, f := range findings {
		if f.Code == codeWorktreeGone {
			count++
			continue
		}
		kept = append(kept, f)
	}
	if count == 0 {
		return kept
	}
	return append(kept, docFinding{
		Code:     codeWorktreeGone,
		Severity: docSeverityInfo,
		Target:   "registry",
		Message: strconv.Itoa(count) + " worktree link(s) point at worktrees that are gone;" +
			" their projects are still present, so the workitems stay linked to them",
		FixHint: "no action needed; `camp workitem links` names them, and" +
			" `camp workitem unlink --id <id>` drops one whose work is finished",
	})
}
