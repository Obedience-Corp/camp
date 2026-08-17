package config

import (
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config/registryfile"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// OrgName is the organization directory name under .config.
const OrgName = "obey"

// AppName is the application name used in config paths.
const AppName = "campaign"

// ConfigDir returns the camp configuration directory.
// Respects XDG_CONFIG_HOME environment variable. It fails rather than returning
// a working-directory-relative path when the home directory cannot be resolved.
func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, OrgName, AppName), nil
	}
	home, err := pathutil.Home()
	if err != nil {
		return "", camperrors.Wrap(err, "resolving camp config directory (set HOME or XDG_CONFIG_HOME)")
	}
	return filepath.Join(home, ".obey", AppName), nil
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// RegistryPath returns the path to the campaign registry file.
// Checks CAMP_REGISTRY_PATH environment variable first for test isolation.
func RegistryPath() (string, error) {
	return registryfile.Path()
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}
