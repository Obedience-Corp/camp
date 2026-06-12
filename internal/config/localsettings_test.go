package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func makeLocalSettingsCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatalf("mkdir .campaign: %v", err)
	}
	return root
}

func chdirLocalSettingsTest(t *testing.T, dir string) {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	campaign.ClearCache()
	invalidateLocalThemeCache()
	t.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
		campaign.ClearCache()
		invalidateLocalThemeCache()
	})
}

func TestLoadLocalSettings_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := LoadLocalSettings(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadLocalSettings() error = %v, want %v", err, context.Canceled)
	}
}

func TestLoadLocalSettings_CorruptFile(t *testing.T) {
	root := makeLocalSettingsCampaign(t)
	path := LocalSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLocalSettings(context.Background(), root)
	if err == nil {
		t.Fatal("LoadLocalSettings() expected error for corrupt JSON")
	}
}

func TestLoadLocalSettings_MissingFileReturnsZeroValue(t *testing.T) {
	root := makeLocalSettingsCampaign(t)

	s, err := LoadLocalSettings(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadLocalSettings() error = %v", err)
	}
	if !s.IsEmpty() {
		t.Fatalf("LoadLocalSettings() = %+v, want zero value", s)
	}
}

func TestSaveLocalSettings_ErrorPaths(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name           string
		ctx            context.Context
		settings       *LocalSettings
		wantCancel     bool
		wantValidation bool
	}{
		{"cancelled context", cancelled, &LocalSettings{}, true, false},
		{"nil settings", context.Background(), nil, false, true},
		{"invalid theme", context.Background(), &LocalSettings{ThemeOverride: "neon"}, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := makeLocalSettingsCampaign(t)
			err := SaveLocalSettings(tt.ctx, root, tt.settings)
			if err == nil {
				t.Fatal("SaveLocalSettings() expected error")
			}
			if tt.wantCancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("SaveLocalSettings() error = %v, want %v", err, context.Canceled)
			}
			if tt.wantValidation {
				var validation *camperrors.ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("SaveLocalSettings() error = %v, want ValidationError", err)
				}
			}
		})
	}
}

func TestSaveLocalSettings_RoundTrip(t *testing.T) {
	root := makeLocalSettingsCampaign(t)
	ctx := context.Background()

	if err := SaveLocalSettings(ctx, root, &LocalSettings{ThemeOverride: ThemeNameDark}); err != nil {
		t.Fatalf("SaveLocalSettings() error = %v", err)
	}

	loaded, err := LoadLocalSettings(ctx, root)
	if err != nil {
		t.Fatalf("LoadLocalSettings() error = %v", err)
	}
	if loaded.ThemeOverride != ThemeNameDark {
		t.Fatalf("ThemeOverride = %q, want %q", loaded.ThemeOverride, ThemeNameDark)
	}
}

func TestSaveLocalSettings_EmptyRemovesFile(t *testing.T) {
	root := makeLocalSettingsCampaign(t)
	ctx := context.Background()

	if err := SaveLocalSettings(ctx, root, &LocalSettings{ThemeOverride: ThemeNameLight}); err != nil {
		t.Fatalf("seed SaveLocalSettings() error = %v", err)
	}
	if _, err := os.Stat(LocalSettingsPath(root)); err != nil {
		t.Fatalf("expected local.json to exist after save: %v", err)
	}

	if err := SaveLocalSettings(ctx, root, &LocalSettings{}); err != nil {
		t.Fatalf("SaveLocalSettings(empty) error = %v", err)
	}
	if _, err := os.Stat(LocalSettingsPath(root)); !os.IsNotExist(err) {
		t.Fatalf("expected local.json removed, stat err = %v", err)
	}
}

func TestSaveLocalSettings_EmptyWithNoFileSucceeds(t *testing.T) {
	root := makeLocalSettingsCampaign(t)

	if err := SaveLocalSettings(context.Background(), root, &LocalSettings{}); err != nil {
		t.Fatalf("SaveLocalSettings(empty, no file) error = %v", err)
	}
}

func TestWithLocalSettingsLock_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := WithLocalSettingsLock(ctx, t.TempDir(), func(s *LocalSettings) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithLocalSettingsLock() error = %v, want %v", err, context.Canceled)
	}
}

func TestWithLocalSettingsLock_FnErrorSkipsSave(t *testing.T) {
	root := makeLocalSettingsCampaign(t)
	ctx := context.Background()
	wantErr := errors.New("boom")

	err := WithLocalSettingsLock(ctx, root, func(s *LocalSettings) error {
		s.ThemeOverride = ThemeNameDark
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithLocalSettingsLock() error = %v, want %v", err, wantErr)
	}
	if _, statErr := os.Stat(LocalSettingsPath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("expected no local.json after fn error, stat err = %v", statErr)
	}
}

func TestWithLocalSettingsLock_SavesMutation(t *testing.T) {
	root := makeLocalSettingsCampaign(t)
	ctx := context.Background()

	err := WithLocalSettingsLock(ctx, root, func(s *LocalSettings) error {
		s.ThemeOverride = ThemeNameHighContrast
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocalSettingsLock() error = %v", err)
	}

	loaded, err := LoadLocalSettings(ctx, root)
	if err != nil {
		t.Fatalf("LoadLocalSettings() error = %v", err)
	}
	if loaded.ThemeOverride != ThemeNameHighContrast {
		t.Fatalf("ThemeOverride = %q, want %q", loaded.ThemeOverride, ThemeNameHighContrast)
	}
}

func TestWithLocalSettingsLock_ClearingDeletesFile(t *testing.T) {
	root := makeLocalSettingsCampaign(t)
	ctx := context.Background()

	if err := SaveLocalSettings(ctx, root, &LocalSettings{ThemeOverride: ThemeNameDark}); err != nil {
		t.Fatalf("seed SaveLocalSettings() error = %v", err)
	}

	err := WithLocalSettingsLock(ctx, root, func(s *LocalSettings) error {
		s.ThemeOverride = ""
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocalSettingsLock() error = %v", err)
	}
	if _, statErr := os.Stat(LocalSettingsPath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("expected local.json removed, stat err = %v", statErr)
	}
}

func TestWithLocalSettingsLock_ConcurrentWritersDoNotCorrupt(t *testing.T) {
	root := makeLocalSettingsCampaign(t)
	ctx := context.Background()
	themes := ValidThemeNames()

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- WithLocalSettingsLock(ctx, root, func(s *LocalSettings) error {
				s.ThemeOverride = themes[i%len(themes)]
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent WithLocalSettingsLock: %v", err)
		}
	}

	loaded, err := LoadLocalSettings(ctx, root)
	if err != nil {
		t.Fatalf("LoadLocalSettings() after concurrent writers: %v", err)
	}
	if !IsValidThemeName(loaded.ThemeOverride) {
		t.Fatalf("ThemeOverride = %q, want a valid theme", loaded.ThemeOverride)
	}
}

func TestIsValidThemeName(t *testing.T) {
	tests := []struct {
		name  string
		theme string
		want  bool
	}{
		{"empty", "", false},
		{"unknown", "neon", false},
		{"case sensitive", "Dark", false},
		{"adaptive", ThemeNameAdaptive, true},
		{"light", ThemeNameLight, true},
		{"dark", ThemeNameDark, true},
		{"high contrast", ThemeNameHighContrast, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidThemeName(tt.theme); got != tt.want {
				t.Fatalf("IsValidThemeName(%q) = %v, want %v", tt.theme, got, tt.want)
			}
		})
	}
}

func TestEffectiveThemeFrom(t *testing.T) {
	tests := []struct {
		name  string
		cfg   *GlobalConfig
		local *LocalSettings
		want  string
	}{
		{"nil inputs", nil, nil, ThemeNameAdaptive},
		{"invalid global falls back", &GlobalConfig{TUI: TUIConfig{Theme: "neon"}}, nil, ThemeNameAdaptive},
		{"invalid override ignored", &GlobalConfig{TUI: TUIConfig{Theme: ThemeNameDark}}, &LocalSettings{ThemeOverride: "neon"}, ThemeNameDark},
		{"global only", &GlobalConfig{TUI: TUIConfig{Theme: ThemeNameLight}}, nil, ThemeNameLight},
		{"empty override inherits", &GlobalConfig{TUI: TUIConfig{Theme: ThemeNameDark}}, &LocalSettings{}, ThemeNameDark},
		{"override wins", &GlobalConfig{TUI: TUIConfig{Theme: ThemeNameDark}}, &LocalSettings{ThemeOverride: ThemeNameLight}, ThemeNameLight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveThemeFrom(tt.cfg, tt.local); got != tt.want {
				t.Fatalf("EffectiveThemeFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEffectiveTheme_OutsideCampaignUsesGlobal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CAMP_ROOT", "")
	chdirLocalSettingsTest(t, t.TempDir())

	ctx := context.Background()
	if err := SaveGlobalConfig(ctx, &GlobalConfig{TUI: TUIConfig{Theme: ThemeNameDark}}); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	if got := EffectiveTheme(ctx); got != ThemeNameDark {
		t.Fatalf("EffectiveTheme() = %q, want %q", got, ThemeNameDark)
	}
}

func TestEffectiveTheme_InsideCampaignWithoutOverrideUsesGlobal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := makeLocalSettingsCampaign(t)
	t.Setenv("CAMP_ROOT", root)
	chdirLocalSettingsTest(t, root)

	ctx := context.Background()
	if err := SaveGlobalConfig(ctx, &GlobalConfig{TUI: TUIConfig{Theme: ThemeNameLight}}); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	if got := EffectiveTheme(ctx); got != ThemeNameLight {
		t.Fatalf("EffectiveTheme() = %q, want %q", got, ThemeNameLight)
	}
}

func TestEffectiveTheme_LocalOverrideWins(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := makeLocalSettingsCampaign(t)
	t.Setenv("CAMP_ROOT", root)
	chdirLocalSettingsTest(t, root)

	ctx := context.Background()
	if err := SaveGlobalConfig(ctx, &GlobalConfig{TUI: TUIConfig{Theme: ThemeNameDark}}); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}
	if err := SaveLocalSettings(ctx, root, &LocalSettings{ThemeOverride: ThemeNameHighContrast}); err != nil {
		t.Fatalf("SaveLocalSettings() error = %v", err)
	}

	if got := EffectiveTheme(ctx); got != ThemeNameHighContrast {
		t.Fatalf("EffectiveTheme() = %q, want %q", got, ThemeNameHighContrast)
	}
}

func TestEffectiveTheme_SaveInvalidatesCachedOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := makeLocalSettingsCampaign(t)
	t.Setenv("CAMP_ROOT", root)
	chdirLocalSettingsTest(t, root)

	ctx := context.Background()
	if err := SaveGlobalConfig(ctx, &GlobalConfig{TUI: TUIConfig{Theme: ThemeNameDark}}); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	if err := SaveLocalSettings(ctx, root, &LocalSettings{ThemeOverride: ThemeNameLight}); err != nil {
		t.Fatalf("SaveLocalSettings() error = %v", err)
	}
	if got := EffectiveTheme(ctx); got != ThemeNameLight {
		t.Fatalf("EffectiveTheme() = %q, want %q", got, ThemeNameLight)
	}

	if err := WithLocalSettingsLock(ctx, root, func(s *LocalSettings) error {
		s.ThemeOverride = ""
		return nil
	}); err != nil {
		t.Fatalf("WithLocalSettingsLock() error = %v", err)
	}
	if got := EffectiveTheme(ctx); got != ThemeNameDark {
		t.Fatalf("EffectiveTheme() after clear = %q, want %q", got, ThemeNameDark)
	}
}

func TestLocalSettingsPath(t *testing.T) {
	got := LocalSettingsPath("/campaign/root")
	want := filepath.Join("/campaign/root", CampaignDir, SettingsDir, LocalSettingsFile)
	if got != want {
		t.Fatalf("LocalSettingsPath() = %q, want %q", got, want)
	}
}

func TestLocalSettings_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		settings *LocalSettings
		want     bool
	}{
		{"nil", nil, true},
		{"zero value", &LocalSettings{}, true},
		{"with override", &LocalSettings{ThemeOverride: ThemeNameDark}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.settings.IsEmpty(); got != tt.want {
				t.Fatalf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateLocalSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings *LocalSettings
		wantErr  bool
	}{
		{"nil", nil, true},
		{"invalid theme", &LocalSettings{ThemeOverride: "neon"}, true},
		{"empty", &LocalSettings{}, false},
		{"valid theme", &LocalSettings{ThemeOverride: ThemeNameLight}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLocalSettings(tt.settings)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateLocalSettings() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				var validation *camperrors.ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("ValidateLocalSettings() error = %v, want ValidationError", err)
				}
			}
		})
	}
}

func TestSaveLocalSettings_FilePersistsValidJSON(t *testing.T) {
	root := makeLocalSettingsCampaign(t)
	ctx := context.Background()

	if err := SaveLocalSettings(ctx, root, &LocalSettings{ThemeOverride: ThemeNameDark}); err != nil {
		t.Fatalf("SaveLocalSettings() error = %v", err)
	}

	data, err := os.ReadFile(LocalSettingsPath(root))
	if err != nil {
		t.Fatalf("read local.json: %v", err)
	}
	want := fmt.Sprintf("{\n  \"theme_override\": %q\n}\n", ThemeNameDark)
	if string(data) != want {
		t.Fatalf("local.json = %q, want %q", data, want)
	}
}
