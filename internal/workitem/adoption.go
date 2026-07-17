package workitem

import camperrors "github.com/Obedience-Corp/camp/internal/errors"

// NeedsAdoption reports whether wi is a builtin doc-type work item (design or
// explore) that was discovered by location but has no .workitem marker, so it
// carries neither a stable id nor a ref. Such an item cannot back a primary
// worktree link: camp p commit would have no ref to stamp, and the link layer
// would try to store the bare "<type>:<path>" key as a workitem id (which its
// validator rejects for containing a slash).
func NeedsAdoption(wi *WorkItem) bool {
	return wi != nil && wi.StableID == "" && IsBuiltinDocType(wi.WorkflowType)
}

// NotAdoptedError reports that the work item at relPath must be adopted before
// it can back a primary worktree link. The message names the exact adopt
// command so the caller can recover in one step.
func NotAdoptedError(relPath string) error {
	return camperrors.NewValidation("workitem",
		"workitem at "+relPath+" is not adopted (no .workitem marker); run "+
			"'camp workitem adopt "+relPath+"' to give it a stable id and ref "+
			"before linking a worktree", nil)
}
