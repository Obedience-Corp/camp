// Package starter owns the one-time starter camp shared by Festival entry points.
package starter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"gopkg.in/yaml.v3"
)

const Name = "festival"
const schemaVersion = 1

type Result struct {
	SchemaVersion int    `json:"schema_version"`
	Action        string `json:"action"`
	Path          string `json:"path,omitempty"`
	Message       string `json:"message,omitempty"`
}

type state struct {
	Version   int    `json:"version"`
	Completed bool   `json:"completed"`
	Path      string `json:"path,omitempty"`
	Staging   string `json:"staging,omitempty"`
	Ready     bool   `json:"ready,omitempty"`
}

// Initialize uses the normal camp init flow without registration. resume is
// true only for a staging directory this operation previously created.
type Initialize func(ctx context.Context, path string, resume bool) error

// Ensure creates or registers a starter only for users with no camps. Staging
// keeps a failed scaffold out of the registry. The journal survives crashes
// before publication, registration, or the final completion write.
func Ensure(ctx context.Context, initialize Initialize) (Result, error) {
	result := Result{SchemaVersion: schemaVersion}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	dir, err := config.ConfigDir()
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return result, camperrors.Wrap(err, "create starter state directory")
	}
	release, err := lock(ctx, filepath.Join(dir, "starter.lock"))
	if err != nil {
		return result, err
	}
	defer release()
	statePath := filepath.Join(dir, "starter.json")
	s := state{Version: schemaVersion}
	data, err := os.ReadFile(statePath)
	if err == nil {
		s = state{}
		if err := json.Unmarshal(data, &s); err != nil {
			return result, camperrors.Wrap(err, "read starter state")
		}
		if err := validateState(s); err != nil {
			return result, err
		}
	} else if !os.IsNotExist(err) {
		return result, camperrors.Wrap(err, "read starter state")
	}
	if s.Completed {
		result.Action = "complete"
		return result, nil
	}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return result, err
	}
	if len(reg.Campaigns) > 0 {
		s.Completed = true
		result.Action = "existing"
		result.Message = remainingStage(s)
		return result, save(ctx, statePath, s)
	}
	if s.Path == "" {
		cfg, err := config.LoadGlobalConfig(ctx)
		if err != nil {
			return result, err
		}
		base, err := cfg.ResolvedCampaignsDir(ctx)
		if err != nil {
			return result, err
		}
		s.Path = filepath.Join(base, Name)
	}
	result.Path = s.Path
	exists, err := targetExists(s.Path)
	if err != nil {
		return result, err
	}
	if exists {
		cfg, err := readExisting(ctx, s.Path)
		if err != nil {
			result.Action = "skipped"
			result.Message = "Starter camp was not created at " + s.Path + ": " + err.Error() + ". Your files were left in place. Choose another camps directory in camp settings, then run camp setup again."
			return result, nil
		}
		return register(ctx, statePath, s, cfg, "registered")
	}
	// Persist the staging path before writing any scaffold, so interruption can
	// resume it without guessing ownership of an existing directory.
	resume := s.Staging != ""
	if !resume {
		if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
			return result, camperrors.Wrap(err, "create camps directory")
		}
		stage, err := os.MkdirTemp(filepath.Dir(s.Path), ".festival-setup-")
		if err != nil {
			return result, camperrors.Wrap(err, "create starter staging directory")
		}
		s.Staging = filepath.Join(stage, Name)
		if err := os.Mkdir(s.Staging, 0o755); err != nil {
			return result, camperrors.Wrap(err, "create starter staging workspace")
		}
		if err := save(ctx, statePath, s); err != nil {
			return result, err
		}
	}
	if !s.Ready {
		if err := initialize(ctx, s.Staging, resume); err != nil {
			return result, camperrors.Wrapf(err, "starter setup incomplete at %s; run camp setup to retry", s.Staging)
		}
		s.Ready = true
		if err := save(ctx, statePath, s); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	cfg, err := config.LoadCampaignConfig(ctx, s.Staging)
	if err != nil {
		return result, err
	}
	// Recheck under the registry lock: an ordinary camp create may have finished
	// while we scaffolded. Never replace another writer's registry snapshot.
	err = config.UpdateRegistry(ctx, func(reg *config.Registry) error {
		if len(reg.Campaigns) > 0 {
			result.Action = "existing"
			return nil
		}
		occupied, err := targetExists(s.Path)
		if err != nil {
			return err
		}
		if occupied {
			return camperrors.New("starter target changed during setup; run camp setup to retry")
		}
		if err := os.Rename(s.Staging, s.Path); err != nil {
			return camperrors.Wrap(err, "publish starter camp")
		}
		result.Action = "created"
		return reg.Register(cfg.ID, cfg.Name, s.Path, cfg.Type)
	})
	if err != nil {
		return result, err
	}
	s.Completed = true
	if result.Action == "created" {
		// Only remove our now-empty staging parent; never remove its contents.
		_ = os.Remove(filepath.Dir(s.Staging))
		s.Staging = ""
	}
	if s.Staging != "" {
		result.Message = "Another camp was registered during setup. Unregistered starter files remain at " + s.Staging
	}
	return result, save(ctx, statePath, s)
}

func register(ctx context.Context, statePath string, s state, cfg *config.CampaignConfig, action string) (Result, error) {
	result := Result{SchemaVersion: schemaVersion, Action: action, Path: s.Path}
	err := config.UpdateRegistry(ctx, func(reg *config.Registry) error {
		if len(reg.Campaigns) > 0 {
			result.Action = "existing"
			return nil
		}
		return reg.Register(cfg.ID, cfg.Name, s.Path, cfg.Type)
	})
	if err != nil {
		return result, err
	}
	s.Completed = true
	result.Message = remainingStage(s)
	return result, save(ctx, statePath, s)
}

func remainingStage(s state) string {
	if s.Staging != "" {
		if _, err := os.Lstat(s.Staging); err == nil {
			return "Unregistered starter files remain at " + s.Staging
		}
	}
	return ""
}

// Empty directories are safe publication targets; files and symlinks are never
// replaced. A symlink is deliberately not followed for automatic setup.
func targetExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, camperrors.Wrap(err, "inspect starter target")
	}
	if !info.IsDir() {
		return true, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, camperrors.Wrap(err, "read starter target")
	}
	return len(entries) != 0, nil
}

func save(ctx context.Context, path string, s state) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "encode starter state")
	}
	return fsutil.WriteFileAtomically(path, append(data, '\n'), 0o600)
}

// Validate existing user data without LoadCampaignConfig's legacy ID backfill.
func readExisting(ctx context.Context, path string) (*config.CampaignConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, camperrors.New("starter target is not a directory")
	}
	data, err := os.ReadFile(config.CampaignConfigPath(path))
	if err != nil {
		return nil, err
	}
	var cfg config.CampaignConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	if cfg.ID == "" {
		return nil, camperrors.New("existing camp has no persisted ID; register it with camp register first")
	}
	if err := config.ValidateCampaignConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateState(s state) error {
	if s.Version != schemaVersion {
		return camperrors.New("unsupported starter state version")
	}
	if s.Path != "" && (!filepath.IsAbs(s.Path) || filepath.Base(s.Path) != Name) {
		return camperrors.New("invalid starter target in setup state")
	}
	if s.Staging != "" {
		parent := filepath.Dir(s.Staging)
		if s.Path == "" || !filepath.IsAbs(s.Staging) || filepath.Base(s.Staging) != Name || filepath.Dir(parent) != filepath.Dir(s.Path) || !strings.HasPrefix(filepath.Base(parent), ".festival-setup-") {
			return camperrors.New("invalid staging path in starter setup state")
		}
	}
	if s.Ready && !s.Completed && s.Staging == "" {
		return camperrors.New("starter setup state is missing its staging path")
	}
	return nil
}
