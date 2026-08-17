package registryfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// File is the on-disk registry representation shared by registry readers.
type File struct {
	Version    int                 `json:"version"`
	DefaultOrg string              `json:"default_org,omitempty"`
	Orgs       []OrgEntry          `json:"orgs,omitempty"`
	Campaigns  map[string]Campaign `json:"campaigns"`
}

// OrgEntry is a persisted org in the registry file. Mirrors config.OrgEntry
// so Load can deserialize orgs without importing config (import cycle).
type OrgEntry struct {
	Name string `json:"name"`
}

// Campaign is the minimal persisted registry campaign shape.
type Campaign struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type,omitempty"`
	LastAccess time.Time `json:"last_access,omitempty"`

	Org    string   `json:"org,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Status string   `json:"status,omitempty"`
}

// Path returns the path to the campaign registry file. It fails rather than
// falling back to a working-directory-relative path when the home directory
// cannot be resolved: a registry written to ./.obey is invisible to the next
// camp invocation from any other directory, which reads as "camp create did not
// register my campaign".
func Path() (string, error) {
	if override := os.Getenv("CAMP_REGISTRY_PATH"); override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "obey", "campaign", "registry.json"), nil
	}
	home, err := pathutil.Home()
	if err != nil {
		return "", camperrors.Wrap(err, "resolving campaign registry path (set HOME, XDG_CONFIG_HOME, or CAMP_REGISTRY_PATH)")
	}
	return filepath.Join(home, ".obey", "campaign", "registry.json"), nil
}

// Load reads the raw registry file from disk. Missing registries return an
// empty File so callers can share the same load path without special casing.
func Load() (*File, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
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
