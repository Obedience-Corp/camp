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
