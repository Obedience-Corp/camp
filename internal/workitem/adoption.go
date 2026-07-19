package workitem

import (
	"regexp"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// NeedsAdoption reports whether wi is a builtin doc-type work item (design or
// explore) that was discovered by location but has no .workitem marker, so it
// carries neither a stable id nor a ref. Such an item cannot back a primary
// worktree link: camp p commit would have no ref to stamp, and the link layer
// would try to store the bare "<type>:<path>" key as a workitem id (which its
// validator rejects for containing a slash).
func NeedsAdoption(wi *WorkItem) bool {
	return wi != nil && wi.StableID == "" && IsBuiltinDocType(wi.WorkflowType)
}

// adoptCommandSafeUnquoted matches paths that are safe to paste into a shell
// unquoted (the kebab-case slugs camp generates under workflow/). A hand-edited
// path outside this set is single-quoted so the recovery command stays
// copy-paste safe, mirroring workitem validate's repairCommandFor.
var adoptCommandSafeUnquoted = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// adoptCommand returns the `camp workitem adopt <path>` recovery command with
// the path shell-quoted when it contains characters (e.g. spaces in a
// hand-created design/explore dir) that would otherwise break a copy-paste.
func adoptCommand(relPath string) string {
	if adoptCommandSafeUnquoted.MatchString(relPath) {
		return "camp workitem adopt " + relPath
	}
	return "camp workitem adopt " + remote.ShellQuote(relPath)
}

// NotAdoptedError reports that the work item at relPath must be adopted before
// it can back a primary worktree link. The message names the exact adopt
// command so the caller can recover in one step.
func NotAdoptedError(relPath string) error {
	return camperrors.NewValidation("workitem",
		"workitem at "+relPath+" is not adopted (no .workitem marker); run "+
			adoptCommand(relPath)+" to give it a stable id and ref "+
			"before linking a worktree", nil)
}
