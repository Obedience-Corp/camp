package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

const LocalSettingsFile = "local.json"

const (
	ThemeNameAdaptive     = "adaptive"
	ThemeNameLight        = "light"
	ThemeNameDark         = "dark"
	ThemeNameHighContrast = "high-contrast"
)

func ValidThemeNames() []string {
	return []string{ThemeNameAdaptive, ThemeNameLight, ThemeNameDark, ThemeNameHighContrast}
}

func IsValidThemeName(name string) bool {
	switch name {
	case ThemeNameAdaptive, ThemeNameLight, ThemeNameDark, ThemeNameHighContrast:
		return true
	}
	return false
}

type LocalSettings struct {
	ThemeOverride string `json:"theme_override,omitempty"`
}

func (s *LocalSettings) IsEmpty() bool {
	return s == nil || *s == LocalSettings{}
}

func LocalSettingsPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, CampaignDir, SettingsDir, LocalSettingsFile)
}

func LoadLocalSettings(ctx context.Context, campaignRoot string) (*LocalSettings, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := LocalSettingsPath(campaignRoot)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &LocalSettings{}, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "failed to read local settings %s", path)
	}

	var s LocalSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, camperrors.Wrapf(err, "failed to parse local settings %s", path)
	}
	return &s, nil
}

func ValidateLocalSettings(s *LocalSettings) error {
	if s == nil {
		return camperrors.NewValidation("local_settings", "cannot save nil local settings", nil)
	}
	if s.ThemeOverride != "" && !IsValidThemeName(s.ThemeOverride) {
		return camperrors.NewValidation("theme_override",
			fmt.Sprintf("unknown theme %q (valid: %s)", s.ThemeOverride, strings.Join(ValidThemeNames(), ", ")), nil)
	}
	return nil
}

func SaveLocalSettings(ctx context.Context, campaignRoot string, s *LocalSettings) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(SettingsDirPath(campaignRoot), 0o755); err != nil {
		return camperrors.Wrap(err, "failed to create settings directory")
	}

	release, err := fsutil.AcquireFileLock(ctx, LocalSettingsPath(campaignRoot)+".lock")
	if err != nil {
		return err
	}
	defer release()

	return saveLocalSettingsLocked(ctx, campaignRoot, s)
}

func saveLocalSettingsLocked(ctx context.Context, campaignRoot string, s *LocalSettings) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateLocalSettings(s); err != nil {
		return err
	}

	path := LocalSettingsPath(campaignRoot)
	if s.IsEmpty() {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return camperrors.Wrapf(err, "failed to remove empty local settings %s", path)
		}
		invalidateLocalThemeCache()
		return nil
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal local settings")
	}
	data = append(data, '\n')

	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "failed to write local settings")
	}
	invalidateLocalThemeCache()
	return nil
}

func WithLocalSettingsLock(ctx context.Context, campaignRoot string, fn func(*LocalSettings) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(SettingsDirPath(campaignRoot), 0o755); err != nil {
		return camperrors.Wrap(err, "failed to create settings directory")
	}

	release, err := fsutil.AcquireFileLock(ctx, LocalSettingsPath(campaignRoot)+".lock")
	if err != nil {
		return err
	}
	defer release()

	s, err := LoadLocalSettings(ctx, campaignRoot)
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return saveLocalSettingsLocked(ctx, campaignRoot, s)
}

type localThemeCache struct {
	mu    sync.Mutex
	root  string
	value string
	valid bool
}

var localTheme localThemeCache

func EffectiveTheme(ctx context.Context) string {
	var cfg *GlobalConfig
	if loaded, err := LoadGlobalConfig(ctx); err == nil {
		cfg = loaded
	}
	return EffectiveThemeFrom(cfg, &LocalSettings{ThemeOverride: localThemeOverride(ctx)})
}

func EffectiveThemeFrom(cfg *GlobalConfig, local *LocalSettings) string {
	theme := ThemeNameAdaptive
	if cfg != nil && IsValidThemeName(cfg.TUI.Theme) {
		theme = cfg.TUI.Theme
	}
	if local != nil && IsValidThemeName(local.ThemeOverride) {
		theme = local.ThemeOverride
	}
	return theme
}

func localThemeOverride(ctx context.Context) string {
	root, err := campaign.DetectCached(ctx)
	if err != nil || root == "" {
		return ""
	}

	localTheme.mu.Lock()
	defer localTheme.mu.Unlock()
	if localTheme.valid && localTheme.root == root {
		return localTheme.value
	}

	value := ""
	if s, loadErr := LoadLocalSettings(ctx, root); loadErr == nil {
		value = s.ThemeOverride
	}
	localTheme.root = root
	localTheme.value = value
	localTheme.valid = true
	return value
}

func invalidateLocalThemeCache() {
	localTheme.mu.Lock()
	defer localTheme.mu.Unlock()
	localTheme.root = ""
	localTheme.value = ""
	localTheme.valid = false
}
