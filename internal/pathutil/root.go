package pathutil

import "path/filepath"

// ResolveRoot canonicalizes a campaign root once before JSON path work.
func ResolveRoot(root string) (string, error) {
	return filepath.EvalSymlinks(root)
}

// RelativeToRoot converts an absolute path to a campaign-root-relative path.
// Relative inputs are cleaned and returned unchanged in meaning.
// LogicalRelativeToRoot returns logicalPath relative to resolvedRoot without
// resolving symlinks below the root. It walks up logicalPath to the first
// ancestor that resolves to resolvedRoot, so a symlinked root still matches,
// and keeps the lexical path from that ancestor down. ok is false when no
// ancestor resolves to the root.
func LogicalRelativeToRoot(resolvedRoot, logicalPath string) (rel string, ok bool) {
	logicalPath = filepath.Clean(logicalPath)
	for dir := logicalPath; ; {
		if real, err := filepath.EvalSymlinks(dir); err == nil && real == resolvedRoot {
			rel, err := filepath.Rel(dir, logicalPath)
			return rel, err == nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func RelativeToRoot(resolvedRoot, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	var resolvedPath string
	if p, err := filepath.EvalSymlinks(path); err == nil {
		resolvedPath = p
	} else {
		resolvedPath = resolveNearestAncestor(path)
	}
	return filepath.Rel(resolvedRoot, resolvedPath)
}
