package links

import (
	"path"
	"path/filepath"
	"strings"
)

// ReleasedOnShelve reports whether shelving the workitem at dirRel (campaign-
// relative, slash form) must release l. Containment is tested as well as
// id/key because a rename re-derives the workitem id while older links keep
// the id they were made with.
func ReleasedOnShelve(l Link, id, key, dirRel string) bool {
	if id != "" && l.WorkitemID == id {
		return true
	}
	if key != "" && l.WorkitemKey == key {
		return true
	}
	return ScopeWithin(l.Scope.Path, dirRel)
}

// ScopeWithin reports whether scopePath is dirRel or nested under it, comparing
// whole path segments so a sibling sharing a prefix does not match. An empty,
// absolute, or escaping path matches nothing rather than everything.
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
