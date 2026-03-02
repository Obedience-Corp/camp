package pathutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ValidateBoundary checks that target is contained within root after resolving
// all symlinks on both paths.
//
// Behavior contract:
//   - root must be a non-empty, resolvable path. Returns ErrBoundaryRootInvalid
//     if root is empty or filepath.EvalSymlinks fails on root.
//   - target may not exist yet (e.g., paths being constructed before creation).
//     When target does not exist, the function applies filepath.Clean and then
//     performs the prefix check without EvalSymlinks on the target itself.
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
		resolvedTarget = filepath.Clean(target)
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
