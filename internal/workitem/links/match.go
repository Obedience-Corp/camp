package links

import (
	"path"
	"path/filepath"
	"strings"
)

// ReleasedOnShelve reports whether l must be released because the workitem that
// held it has been shelved. dirRel is the campaign-relative path the workitem
// occupied before the move, in slash form; id and key are its stable workitem id
// and workitem key, either of which may be empty for an unadopted source.
//
// The path arm exists because identity and location drift apart. A link records
// the workitem id that resolved at link time; renaming the directory afterward
// re-derives a different id from the new slug, so an id/key comparison alone
// leaves the older links behind, still pointing at a path that the shelve just
// emptied. That is exactly what happened on 2026-08-20: two links scoped to
// workflow/explore/firstmate-competitive-analysis carried the id the directory
// had under its previous name, survived the sweep, and turned into broken links
// that camp workitem doctor --fix had to clean up. A link scoped inside the
// moved directory is orphaned by the move regardless of which id it names, so
// containment is the honest test.
func ReleasedOnShelve(l Link, id, key, dirRel string) bool {
	if id != "" && l.WorkitemID == id {
		return true
	}
	if key != "" && l.WorkitemKey == key {
		return true
	}
	return ScopeWithin(l.Scope.Path, dirRel)
}

// ScopeWithin reports whether scopePath is dirRel itself or nested under it.
// Both are campaign-relative paths; every scope kind carries one, so this is
// applied uniformly rather than per kind (a worktree scope simply never lands
// under a workflow directory, and the containment test says so on its own).
//
// Comparison is per path segment: workflow/explore/firstmate-notes is NOT under
// workflow/explore/firstmate, even though one is a string prefix of the other.
// An empty, absolute, or escaping path on either side matches nothing, so a
// caller that could not determine where the workitem lived releases no links by
// path rather than releasing every link in the registry.
func ScopeWithin(scopePath, dirRel string) bool {
	dir := normalizeScopePath(dirRel)
	scope := normalizeScopePath(scopePath)
	if dir == "" || scope == "" {
		return false
	}
	if scope == dir {
		return true
	}
	return strings.HasPrefix(scope, dir+"/")
}

// normalizeScopePath reduces p to a clean campaign-relative slash path, or ""
// when it does not name one.
func normalizeScopePath(p string) string {
	p = strings.TrimSpace(filepath.ToSlash(p))
	if p == "" || strings.HasPrefix(p, "/") {
		return ""
	}
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}
