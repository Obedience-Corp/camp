package machines

import (
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// MachinesPath returns the path to ~/.obey/machines.yaml, the top-level sibling
// of camp's campaign config dir (NOT inside it). It mirrors the resolution used
// by the campaign registry (internal/config/registryfile.Path): an explicit
// override wins, then XDG_CONFIG_HOME, then the home directory, so the file
// tracks the same base as the rest of camp's state and tests can isolate it.
// Like the registry, it fails rather than resolving to a working-directory-
// relative path when no home directory can be determined.
func MachinesPath() (string, error) {
	if override := os.Getenv("CAMP_MACHINES_PATH"); override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "obey", "machines.yaml"), nil
	}
	home, err := pathutil.Home()
	if err != nil {
		return "", camperrors.Wrap(err, "resolving machines file path (set HOME, XDG_CONFIG_HOME, or CAMP_MACHINES_PATH)")
	}
	return filepath.Join(home, ".obey", "machines.yaml"), nil
}

// DisplayPath returns MachinesPath for message text, falling back to the
// canonical location when resolution fails so prompts and hints stay readable.
func DisplayPath() string {
	path, err := MachinesPath()
	if err != nil {
		return "~/.obey/machines.yaml"
	}
	return path
}
