package pathutil

import (
	"os"
	"strings"
)

// AbbreviateHome replaces a leading $HOME in path with "~" for display. It
// returns path unchanged when HOME is unknown or path is not under it. An exact
// home match returns "~". This is display-only; "~" re-expands in any shell.
func AbbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}
