package dirlink

import (
	"path/filepath"
	"strings"
)

// uncPath reports whether absTarget is a UNC path that requires a directory
// symlink on Windows (junctions cannot represent network targets).
func uncPath(absTarget string) bool {
	clean := filepath.Clean(absTarget)
	return strings.HasPrefix(clean, `\\`)
}
