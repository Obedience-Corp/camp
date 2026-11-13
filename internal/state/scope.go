package state

import (
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// InsideRoot reports whether location belongs to the camp at campaignRoot.
// A location counts when its symlink-resolved path is under the resolved root,
// or when its logical path is, which keeps linked projects (symlinks or marker
// files that resolve elsewhere) inside the camp they are linked into.
func InsideRoot(campaignRoot, location string) bool {
	if campaignRoot == "" || location == "" || !filepath.IsAbs(location) {
		return false
	}
	resolvedRoot, err := pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		return false
	}
	if rel, err := pathutil.RelativeToRoot(resolvedRoot, location); err == nil && relWithin(rel) {
		return true
	}
	rel, ok := pathutil.LogicalRelativeToRoot(resolvedRoot, location)
	return ok && relWithin(rel)
}

func relWithin(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
