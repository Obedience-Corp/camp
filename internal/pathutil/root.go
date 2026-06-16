package pathutil

import "path/filepath"

// ResolveRoot canonicalizes a campaign root once before JSON path work.
func ResolveRoot(root string) (string, error) {
	return filepath.EvalSymlinks(root)
}

// RelativeToRoot converts an absolute path to a campaign-root-relative path.
// Relative inputs are cleaned and returned unchanged in meaning.
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
