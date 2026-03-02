package pathutil

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
	panic("not yet implemented — see task 02_implement_boundary_check")
}
