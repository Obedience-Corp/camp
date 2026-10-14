package dirlink

import (
	"os"
)

// DirEntryIsDirectoryLink reports whether entry at fullPath is a directory link.
// ReadDir may omit ModeSymlink for Windows junctions, so fullPath is required.
func DirEntryIsDirectoryLink(entry os.DirEntry, fullPath string) (bool, error) {
	if entry.Type()&os.ModeSymlink != 0 {
		return true, nil
	}
	return IsDirectoryLink(fullPath)
}
