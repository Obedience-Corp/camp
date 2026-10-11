package fresh

// pruneBaseRef returns the ref the prune step measures merges against.
//
// A real run prunes after the sync step: the default branch is checked out and
// level with origin/<default>, so the sync base is already current. A dry-run
// moves no local ref. Its local default branch still sits where it was before
// the fetch, and measuring against it hides every branch that landed upstream
// since then. The preview therefore measures against origin/<default>, which
// the fetch did refresh and which is exactly where the sync step would leave
// the default branch.
//
// When the project syncs detached, the base is origin/<default> in both modes.
func (s freshSyncState) pruneBaseRef(dryRun bool) string {
	if dryRun && !s.detached {
		return "origin/" + s.defaultBranch
	}
	return s.baseRef
}
