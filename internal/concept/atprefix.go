package concept

import (
	"fmt"
	"strings"
)

// DefaultAtPrefixes maps @ shortcuts to campaign-relative directory paths.
var DefaultAtPrefixes = map[string]string{
	"@p": "projects",
	"@w": "workflow",
	"@f": "festivals",
	"@d": "docs",
}

// ResolveAtPath expands @ prefix shortcuts in a path string.
// For example, "@p/fest" becomes "projects/fest".
// Paths without @ prefix pass through unchanged.
func ResolveAtPath(path string) (string, error) {
	if !strings.HasPrefix(path, "@") {
		return path, nil
	}

	// Extract the @ prefix (up to the first / or end of string)
	prefix := path
	rest := ""
	if idx := strings.IndexByte(path, '/'); idx != -1 {
		prefix = path[:idx]
		rest = path[idx+1:]
	}

	resolved, ok := DefaultAtPrefixes[prefix]
	if !ok {
		return "", fmt.Errorf("unknown concept shortcut: %s (valid: @p, @w, @f, @d)", prefix)
	}

	if rest == "" {
		return resolved, nil
	}
	return resolved + "/" + rest, nil
}
