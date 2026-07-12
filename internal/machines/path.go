package machines

import (
	"os"
	"path/filepath"
)

// MachinesPath returns the path to ~/.obey/machines.yaml, the top-level sibling
// of camp's campaign config dir (NOT inside it). It mirrors the resolution used
// by the campaign registry (internal/config/registryfile.Path): an explicit
// override wins, then XDG_CONFIG_HOME, then the home directory, so the file
// tracks the same base as the rest of camp's state and tests can isolate it.
func MachinesPath() string {
	if override := os.Getenv("CAMP_MACHINES_PATH"); override != "" {
		return override
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "obey", "machines.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".obey", "machines.yaml")
}
