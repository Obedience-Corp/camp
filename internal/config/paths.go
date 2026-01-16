package config

import (
	"os"
	"path/filepath"
)

// AppName is the application name used in config paths.
const AppName = "campaign"

// ConfigDir returns the camp configuration directory.
// Respects XDG_CONFIG_HOME environment variable.
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, AppName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", AppName)
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// RegistryPath returns the path to the campaign registry file.
func RegistryPath() string {
	return filepath.Join(ConfigDir(), "registry.yaml")
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	return os.MkdirAll(ConfigDir(), 0755)
}
