package registryfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// File is the on-disk registry representation shared by registry readers.
type File struct {
	Version   int                 `json:"version"`
	Campaigns map[string]Campaign `json:"campaigns"`
}

// Campaign is the minimal persisted registry campaign shape.
type Campaign struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type,omitempty"`
	LastAccess time.Time `json:"last_access,omitempty"`
}

// Path returns the path to the campaign registry file.
func Path() string {
	if override := os.Getenv("CAMP_REGISTRY_PATH"); override != "" {
		return override
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "obey", "campaign", "registry.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".obey", "campaign", "registry.json")
}

// Load reads the raw registry file from disk. Missing registries return an
// empty File so callers can share the same load path without special casing.
func Load() (*File, error) {
	path := Path()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Campaigns: make(map[string]Campaign)}, nil
		}
		return nil, err
	}

	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Campaigns == nil {
		file.Campaigns = make(map[string]Campaign)
	}

	return &file, nil
}
