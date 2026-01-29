package config

import (
	"os"
	"path/filepath"
)

// OrgName is the organization directory name under .config.
const OrgName = "obey"

// AppName is the application name used in config paths.
const AppName = "campaign"

// ConfigDir returns the camp configuration directory.
// Respects XDG_CONFIG_HOME environment variable.
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, OrgName, AppName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", OrgName, AppName)
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// RegistryPath returns the path to the campaign registry file.
// Checks CAMP_REGISTRY_PATH environment variable first for test isolation.
func RegistryPath() string {
	if override := os.Getenv("CAMP_REGISTRY_PATH"); override != "" {
		return override
	}
	return filepath.Join(ConfigDir(), "registry.json")
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	return os.MkdirAll(ConfigDir(), 0755)
}
