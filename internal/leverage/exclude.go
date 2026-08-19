package leverage

import (
	"path/filepath"
	"strings"
)

// excludeSet matches repo-relative paths against excluded directories.
//
// It mirrors the semantics already applied to scc scans so blame and LOC
// counting agree on what counts as authored code. Two entry shapes are
// supported, matching the two producers of exclude lists:
//
//   - Bare names (DefaultExcludeDirs: "node_modules", "vendor", ...) match a
//     directory of that name at any depth, the same way scc --exclude-dir does.
//   - Relative paths (submodule paths recorded on monorepo roots) match only
//     that subtree, so a submodule named "build" does not silently exclude
//     every build directory in the repo.
type excludeSet struct {
	names    map[string]struct{}
	prefixes []string
}

// newExcludeSet builds a matcher from DefaultExcludeDirs plus the project's
// own exclusions.
func newExcludeSet(extra []string) *excludeSet {
	merged := mergeExcludeDirs(DefaultExcludeDirs, extra)

	set := &excludeSet{names: make(map[string]struct{}, len(merged))}
	for _, d := range merged {
		clean := strings.Trim(filepath.ToSlash(strings.TrimSpace(d)), "/")
		if clean == "" || clean == "." {
			continue
		}
		if strings.Contains(clean, "/") {
			set.prefixes = append(set.prefixes, clean+"/")
			continue
		}
		set.names[clean] = struct{}{}
	}
	return set
}

// excluded reports whether a repo-relative file path lies inside an excluded
// directory. The final path element is the file itself and is never treated as
// a directory match.
func (e *excludeSet) excluded(path string) bool {
	p := filepath.ToSlash(path)

	for _, prefix := range e.prefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}

	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return false // no parent directories to match
	}
	dir := p[:idx]

	for _, part := range strings.Split(dir, "/") {
		if _, ok := e.names[part]; ok {
			return true
		}
	}
	return false
}

// filterExcluded drops paths that fall inside vendored, generated, or
// submodule directories.
func filterExcluded(paths, extra []string) []string {
	set := newExcludeSet(extra)

	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if !set.excluded(p) {
			kept = append(kept, p)
		}
	}
	return kept
}
