package workitem

import (
	"errors"
	"os"
	"path/filepath"
)

var builtinTypes = map[WorkflowType]bool{
	WorkflowTypeIntent:   true,
	WorkflowTypeDesign:   true,
	WorkflowTypeExplore:  true,
	WorkflowTypeFestival: true,
}

func emitCandidate(typeDir, dir string) (bool, string) {
	if builtinTypes[WorkflowType(typeDir)] {
		return true, "builtin"
	}
	markerPath := filepath.Join(dir, MetadataFilename)
	if _, err := os.Stat(markerPath); err == nil {
		return true, "marker"
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, "stat-error"
	}
	return false, "no-marker"
}
