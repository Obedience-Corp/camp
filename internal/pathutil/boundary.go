package pathutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// resolveNearestAncestor resolves symlinks on the nearest existing ancestor of
// path, then joins back the non-existent suffix. This handles the macOS case
// where /var is a symlink to /private/var: a non-existent target like
// /var/folders/.../myproject gets correctly resolved to /private/var/folders/.../myproject
// by resolving the existing /var/folders/... ancestor.
func resolveNearestAncestor(path string) string {
	cleaned := filepath.Clean(path)
	current := cleaned
	var suffix string

	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if suffix == "" {
				return resolved
			}
			return filepath.Join(resolved, suffix)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return cleaned
		}

		base := filepath.Base(current)
		if suffix == "" {
			suffix = base
		} else {
			suffix = filepath.Join(base, suffix)
		}
		current = parent
	}
}

// ValidateBoundary checks that target is contained within root after resolving
// all symlinks on both paths.
//
// Behavior contract:
//   - root must be a non-empty, resolvable path. Returns ErrBoundaryRootInvalid
//     if root is empty or filepath.EvalSymlinks fails on root.
//   - target may not exist yet (e.g., paths being constructed before creation).
//     When target does not exist, the function resolves symlinks on the nearest
//     existing ancestor directory and joins back the non-existent suffix. This
//     handles the macOS /var -> /private/var case correctly.
//     When target does exist, both root and target are fully resolved.
//   - The containment check uses filepath.Rel to obtain a relative path from
//     root to target, then rejects any result beginning with "..".
//   - Returns *BoundaryError wrapping ErrOutsideBoundary on violation.
//   - Returns nil when target is at or under root.
//
// Example:
//
//	err := pathutil.ValidateBoundary("/campaign", "/campaign/projects/my-proj")
//	// err == nil
//
//	err = pathutil.ValidateBoundary("/campaign", "/campaign/../etc/passwd")
//	// err wraps ErrOutsideBoundary
func ValidateBoundary(root, target string) error {
	if root == "" {
		return &BoundaryError{Root: root, Target: target, Cause: ErrBoundaryRootInvalid}
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return &BoundaryError{Root: root, Target: target, Cause: ErrBoundaryRootInvalid}
	}

	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return &BoundaryError{Root: resolvedRoot, Target: target, Cause: ErrOutsideBoundary}
		}
		resolvedTarget = resolveNearestAncestor(target)
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil {
		return &BoundaryError{Root: resolvedRoot, Target: resolvedTarget, Cause: ErrOutsideBoundary}
	}

	if strings.HasPrefix(rel, "..") {
		return &BoundaryError{Root: resolvedRoot, Target: resolvedTarget, Cause: ErrOutsideBoundary}
	}

	return nil
}
