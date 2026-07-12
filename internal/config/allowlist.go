package config

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// AllowlistFile is the name of the tool allowlist within .campaign/settings/.
const AllowlistFile = "allowlist.json"

// AllowlistVersion is the current allowlist format version.
const AllowlistVersion = 1

// Allowlist is the on-disk .campaign/settings/allowlist.json shape: the tools a
// campaign permits agents to run, plus whether built-in defaults are inherited.
type Allowlist struct {
	Version         int                         `json:"version"`
	Commands        map[string]AllowlistCommand `json:"commands"`
	InheritDefaults bool                        `json:"inherit_defaults"`
}

// AllowlistCommand is a single allowlist entry.
type AllowlistCommand struct {
	Allowed     bool   `json:"allowed"`
	Description string `json:"description,omitempty"`
}

// AllowlistPath returns the allowlist file path for a campaign.
func AllowlistPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, CampaignDir, SettingsDir, AllowlistFile)
}

// LoadAllowlist reads .campaign/settings/allowlist.json. A missing file returns
// an empty, defaults-inheriting allowlist so callers can start adding entries.
func LoadAllowlist(ctx context.Context, campaignRoot string) (*Allowlist, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := AllowlistPath(campaignRoot)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Allowlist{Version: AllowlistVersion, Commands: map[string]AllowlistCommand{}, InheritDefaults: true}, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "failed to read allowlist %s", path)
	}

	var al Allowlist
	if err := json.Unmarshal(data, &al); err != nil {
		return nil, camperrors.Wrapf(err, "failed to parse allowlist %s", path)
	}
	if al.Commands == nil {
		al.Commands = map[string]AllowlistCommand{}
	}
	if al.Version == 0 {
		al.Version = AllowlistVersion
	}
	return &al, nil
}

// SaveAllowlist writes the allowlist atomically under a file lock.
func SaveAllowlist(ctx context.Context, campaignRoot string, al *Allowlist) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if al == nil {
		return camperrors.NewValidation("allowlist", "cannot save nil allowlist", nil)
	}

	if err := os.MkdirAll(SettingsDirPath(campaignRoot), 0o755); err != nil {
		return camperrors.Wrap(err, "failed to create settings directory")
	}

	path := AllowlistPath(campaignRoot)
	release, err := fsutil.AcquireFileLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	if al.Version == 0 {
		al.Version = AllowlistVersion
	}

	data, err := json.MarshalIndent(al, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal allowlist")
	}
	data = append(data, '\n')

	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "failed to write allowlist")
	}
	return nil
}
