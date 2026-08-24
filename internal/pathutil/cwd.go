package pathutil

import (
	"os"
	"path/filepath"
)

// LogicalCwd returns the shell's logical working directory when the process
// cwd was entered through a symlink. os.Getwd may return the physical target
// path, which loses the symlink context a caller needs to match against a
// logical path (e.g. projects/worktrees/<project> when that holder is a
// symlink).
//
// PWD is only trusted when it is absolute, exists, and resolves to the same
// physical directory as os.Getwd. If any of those conditions fail, the
// physical cwd from os.Getwd is returned.
func LogicalCwd() (string, error) {
	physical, err := os.Getwd()
	if err != nil {
		return "", err
	}
	physical, err = filepath.Abs(physical)
	if err != nil {
		return "", err
	}
	physical = filepath.Clean(physical)

	logical := os.Getenv("PWD")
	if logical == "" || !filepath.IsAbs(logical) {
		return physical, nil
	}
	logical, err = filepath.Abs(logical)
	if err != nil {
		return physical, nil
	}
	logical = filepath.Clean(logical)
	if _, err := os.Stat(logical); err != nil {
		return physical, nil
	}

	resolvedPhysical, err := filepath.EvalSymlinks(physical)
	if err != nil {
		return physical, nil
	}
	resolvedLogical, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return physical, nil
	}
	if resolvedPhysical == resolvedLogical {
		return logical, nil
	}

	return physical, nil
}
