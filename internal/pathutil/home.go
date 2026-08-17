package pathutil

import (
	"os"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Home returns the current user's home directory, treating an empty result as
// an error rather than a valid answer. os.UserHomeDir returns ("", err) when
// HOME is unset, and every caller that ignored that error joined state paths
// onto "" — producing a path relative to the working directory, so camp wrote
// its registry, config, or machines file into whatever directory it happened to
// be run from and could not find it again from anywhere else. Callers wrap this
// with the override that applies to the file they are resolving.
func Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", camperrors.Wrap(err, "cannot determine home directory")
	}
	if strings.TrimSpace(home) == "" {
		return "", camperrors.New("cannot determine home directory: $HOME is not set")
	}
	return home, nil
}

// AbbreviateHome replaces a leading $HOME in path with "~" for display. It
// returns path unchanged when HOME is unknown or path is not under it. An exact
// home match returns "~". This is display-only; "~" re-expands in any shell.
func AbbreviateHome(path string) string {
	home, err := Home()
	if err != nil {
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
