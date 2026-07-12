package workitem

import (
	"errors"
	"io/fs"
	"path/filepath"
)

var builtinTypes = map[WorkflowType]bool{
	WorkflowTypeIntent:   true,
	WorkflowTypeDesign:   true,
	WorkflowTypeExplore:  true,
	WorkflowTypeFestival: true,
}

// IsBuiltinType reports whether t is a builtin workflow type with dedicated
// discovery semantics. Custom types only surface work items when an explicit
// .workitem marker is present.
func IsBuiltinType(t WorkflowType) bool {
	return builtinTypes[t]
}

// IsBuiltinDocType reports whether t is a builtin directory-doc workflow type
// (design or explore) whose child directories are treated as work items by
// location, without requiring a .workitem marker.
func IsBuiltinDocType(t WorkflowType) bool {
	return t == WorkflowTypeDesign || t == WorkflowTypeExplore
}

func emitCandidateFS(fsys fs.FS, typeDir, dir string) (bool, string) {
	if builtinTypes[WorkflowType(typeDir)] {
		return true, "builtin"
	}
	markerPath := filepath.Join(dir, MetadataFilename)
	if _, err := fs.Stat(fsys, markerPath); err == nil {
		return true, "marker"
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, "stat-error"
	}
	return false, "no-marker"
}
