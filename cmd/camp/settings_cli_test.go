package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func execSettingsCmd(t *testing.T, newCmd func() *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	cmd := newCmd()
	cmd.SetContext(context.Background())
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func setupSettingsTest(t *testing.T, inCampaign bool) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if !inCampaign {
		t.Setenv("CAMP_ROOT", "")
		chdirForTest(t, t.TempDir())
		return ""
	}
	root := makeTestCampaign(t, "settings-test-campaign")
	t.Setenv("CAMP_ROOT", root)
	chdirForTest(t, root)
	return root
}

func TestSettingsSet_ErrorPaths(t *testing.T) {
	tests := []struct {
		name       string
		inCampaign bool
		args       []string
	}{
		{"unknown key", true, []string{"bogus.key", "x"}},
		{"invalid global theme", true, []string{"global.theme", "neon"}},
		{"invalid verbose bool", true, []string{"global.verbose", "maybe"}},
		{"invalid no_color bool", true, []string{"global.no_color", "2x"}},
		{"invalid local theme", true, []string{"local.theme_override", "neon"}},
		{"local key outside campaign", false, []string{"local.theme_override", "dark"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupSettingsTest(t, tt.inCampaign)

			_, _, err := execSettingsCmd(t, newSettingsSetCmd, tt.args...)
			if err == nil {
				t.Fatal("settings set expected error")
			}
			var validation *camperrors.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("settings set error = %v, want ValidationError", err)
			}
		})
	}
}

func TestSettingsSet_GlobalValues(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		value  string
		verify func(t *testing.T, cfg *config.GlobalConfig)
	}{
		{"theme", "global.theme", "dark", func(t *testing.T, cfg *config.GlobalConfig) {
			if cfg.TUI.Theme != config.ThemeNameDark {
				t.Fatalf("TUI.Theme = %q, want %q", cfg.TUI.Theme, config.ThemeNameDark)
			}
		}},
		{"theme normalizes case", "global.theme", "LIGHT", func(t *testing.T, cfg *config.GlobalConfig) {
			if cfg.TUI.Theme != config.ThemeNameLight {
				t.Fatalf("TUI.Theme = %q, want %q", cfg.TUI.Theme, config.ThemeNameLight)
			}
		}},
		{"editor", "global.editor", "nvim", func(t *testing.T, cfg *config.GlobalConfig) {
			if cfg.Editor != "nvim" {
				t.Fatalf("Editor = %q, want nvim", cfg.Editor)
			}
		}},
		{"campaigns_dir", "global.campaigns_dir", "~/work/campaigns", func(t *testing.T, cfg *config.GlobalConfig) {
			if cfg.CampaignsDir != "~/work/campaigns" {
				t.Fatalf("CampaignsDir = %q, want ~/work/campaigns", cfg.CampaignsDir)
			}
		}},
		{"verbose", "global.verbose", "true", func(t *testing.T, cfg *config.GlobalConfig) {
			if !cfg.Verbose {
				t.Fatal("Verbose = false, want true")
			}
		}},
		{"no_color", "global.no_color", "true", func(t *testing.T, cfg *config.GlobalConfig) {
			if !cfg.NoColor {
				t.Fatal("NoColor = false, want true")
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupSettingsTest(t, false)

			out, _, err := execSettingsCmd(t, newSettingsSetCmd, tt.key, tt.value)
			if err != nil {
				t.Fatalf("settings set error = %v", err)
			}
			if !strings.Contains(out, "set "+tt.key) {
				t.Fatalf("settings set output = %q, want confirmation for %s", out, tt.key)
			}

			cfg, err := config.LoadGlobalConfig(context.Background())
			if err != nil {
				t.Fatalf("LoadGlobalConfig() error = %v", err)
			}
			tt.verify(t, cfg)
		})
	}
}

func TestSettingsSet_LocalThemeOverrideWritesFile(t *testing.T) {
	root := setupSettingsTest(t, true)

	out, _, err := execSettingsCmd(t, newSettingsSetCmd, "local.theme_override", "light")
	if err != nil {
		t.Fatalf("settings set error = %v", err)
	}
	if !strings.Contains(out, "set local.theme_override = light") {
		t.Fatalf("settings set output = %q", out)
	}

	local, err := config.LoadLocalSettings(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadLocalSettings() error = %v", err)
	}
	if local.ThemeOverride != config.ThemeNameLight {
		t.Fatalf("ThemeOverride = %q, want %q", local.ThemeOverride, config.ThemeNameLight)
	}
}

func TestSettingsSet_LocalThemeOverrideInheritDeletesFile(t *testing.T) {
	root := setupSettingsTest(t, true)

	if _, _, err := execSettingsCmd(t, newSettingsSetCmd, "local.theme_override", "dark"); err != nil {
		t.Fatalf("seed settings set error = %v", err)
	}
	if _, err := os.Stat(config.LocalSettingsPath(root)); err != nil {
		t.Fatalf("expected local.json after set: %v", err)
	}

	out, _, err := execSettingsCmd(t, newSettingsSetCmd, "local.theme_override", "inherit")
	if err != nil {
		t.Fatalf("settings set inherit error = %v", err)
	}
	if !strings.Contains(out, "cleared local.theme_override") {
		t.Fatalf("settings set output = %q", out)
	}
	if _, err := os.Stat(config.LocalSettingsPath(root)); !os.IsNotExist(err) {
		t.Fatalf("expected local.json removed, stat err = %v", err)
	}
}

func TestSettingsGet_ErrorPaths(t *testing.T) {
	tests := []struct {
		name       string
		inCampaign bool
		args       []string
	}{
		{"unknown key", true, []string{"bogus.key"}},
		{"local key outside campaign", false, []string{"local.theme_override"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupSettingsTest(t, tt.inCampaign)

			_, _, err := execSettingsCmd(t, newSettingsGetCmd, tt.args...)
			if err == nil {
				t.Fatal("settings get expected error")
			}
			var validation *camperrors.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("settings get error = %v, want ValidationError", err)
			}
		})
	}
}

func TestSettingsGet_AllKeysText(t *testing.T) {
	setupSettingsTest(t, true)

	if _, _, err := execSettingsCmd(t, newSettingsSetCmd, "global.theme", "dark"); err != nil {
		t.Fatalf("seed set theme: %v", err)
	}
	if _, _, err := execSettingsCmd(t, newSettingsSetCmd, "local.theme_override", "light"); err != nil {
		t.Fatalf("seed set override: %v", err)
	}

	out, _, err := execSettingsCmd(t, newSettingsGetCmd)
	if err != nil {
		t.Fatalf("settings get error = %v", err)
	}

	wantLines := []string{
		"global.theme = dark",
		"global.editor = ",
		"global.campaigns_dir = ",
		"global.verbose = false",
		"global.no_color = false",
		"local.theme_override = light",
		"effective.theme = light",
	}
	for _, line := range wantLines {
		if !strings.Contains(out, line) {
			t.Errorf("settings get output missing %q\noutput:\n%s", line, out)
		}
	}
}

func TestSettingsGet_AllKeysTextOutsideCampaign(t *testing.T) {
	setupSettingsTest(t, false)

	out, _, err := execSettingsCmd(t, newSettingsGetCmd)
	if err != nil {
		t.Fatalf("settings get error = %v", err)
	}
	if strings.Contains(out, "local.theme_override") {
		t.Fatalf("settings get outside campaign should omit local keys, got:\n%s", out)
	}
	if !strings.Contains(out, "effective.theme = adaptive") {
		t.Fatalf("settings get output missing effective theme, got:\n%s", out)
	}
}

func TestSettingsGet_SingleKey(t *testing.T) {
	setupSettingsTest(t, true)

	if _, _, err := execSettingsCmd(t, newSettingsSetCmd, "global.theme", "high-contrast"); err != nil {
		t.Fatalf("seed set theme: %v", err)
	}

	out, _, err := execSettingsCmd(t, newSettingsGetCmd, "global.theme")
	if err != nil {
		t.Fatalf("settings get error = %v", err)
	}
	if strings.TrimSpace(out) != config.ThemeNameHighContrast {
		t.Fatalf("settings get global.theme = %q, want %q", strings.TrimSpace(out), config.ThemeNameHighContrast)
	}
}

func TestSettingsGet_JSONShape(t *testing.T) {
	setupSettingsTest(t, true)

	if _, _, err := execSettingsCmd(t, newSettingsSetCmd, "global.theme", "dark"); err != nil {
		t.Fatalf("seed set theme: %v", err)
	}
	if _, _, err := execSettingsCmd(t, newSettingsSetCmd, "local.theme_override", "light"); err != nil {
		t.Fatalf("seed set override: %v", err)
	}

	out, _, err := execSettingsCmd(t, newSettingsGetCmd, "--json")
	if err != nil {
		t.Fatalf("settings get --json error = %v", err)
	}

	var payload settingsPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("settings get --json invalid JSON: %v\nraw: %s", err, out)
	}
	if payload.SchemaVersion != SettingsJSONVersion {
		t.Errorf("schema_version = %q, want %q", payload.SchemaVersion, SettingsJSONVersion)
	}
	if payload.GeneratedAt.IsZero() {
		t.Error("generated_at is zero")
	}
	if !payload.InCampaign {
		t.Error("in_campaign = false, want true")
	}
	if payload.Global.Theme != config.ThemeNameDark {
		t.Errorf("global.theme = %q, want %q", payload.Global.Theme, config.ThemeNameDark)
	}
	if payload.Local == nil || payload.Local.ThemeOverride != config.ThemeNameLight {
		t.Errorf("local = %+v, want theme_override %q", payload.Local, config.ThemeNameLight)
	}
	if payload.Effective.Theme != config.ThemeNameLight {
		t.Errorf("effective.theme = %q, want %q", payload.Effective.Theme, config.ThemeNameLight)
	}
}

func TestSettingsGet_JSONOutsideCampaignOmitsLocal(t *testing.T) {
	setupSettingsTest(t, false)

	out, _, err := execSettingsCmd(t, newSettingsGetCmd, "--json")
	if err != nil {
		t.Fatalf("settings get --json error = %v", err)
	}

	var payload settingsPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("settings get --json invalid JSON: %v\nraw: %s", err, out)
	}
	if payload.InCampaign {
		t.Error("in_campaign = true, want false")
	}
	if payload.Local != nil {
		t.Errorf("local = %+v, want omitted", payload.Local)
	}
}

func TestSettingsGet_JSONSingleKey(t *testing.T) {
	setupSettingsTest(t, true)

	if _, _, err := execSettingsCmd(t, newSettingsSetCmd, "global.verbose", "true"); err != nil {
		t.Fatalf("seed set verbose: %v", err)
	}

	out, _, err := execSettingsCmd(t, newSettingsGetCmd, "global.verbose", "--json")
	if err != nil {
		t.Fatalf("settings get --json error = %v", err)
	}

	var payload settingsValuePayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("settings get --json invalid JSON: %v\nraw: %s", err, out)
	}
	if payload.SchemaVersion != SettingsJSONVersion {
		t.Errorf("schema_version = %q, want %q", payload.SchemaVersion, SettingsJSONVersion)
	}
	if payload.Key != "global.verbose" {
		t.Errorf("key = %q, want global.verbose", payload.Key)
	}
	if value, ok := payload.Value.(bool); !ok || !value {
		t.Errorf("value = %v, want true", payload.Value)
	}
}

func TestSettingsGet_JSONErrorEnvelope(t *testing.T) {
	setupSettingsTest(t, true)

	_, errOut, err := execSettingsCmd(t, newSettingsGetCmd, "bogus.key", "--json")
	if err == nil {
		t.Fatal("settings get expected error")
	}

	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if jsonErr := json.Unmarshal([]byte(errOut), &envelope); jsonErr != nil {
		t.Fatalf("stderr is not a JSON envelope: %v\nraw: %s", jsonErr, errOut)
	}
	if envelope.SchemaVersion != SettingsJSONVersion {
		t.Errorf("schema_version = %q, want %q", envelope.SchemaVersion, SettingsJSONVersion)
	}
	if envelope.Error.Code != "validation_error" {
		t.Errorf("error.code = %q, want validation_error", envelope.Error.Code)
	}
}

func TestSettingsSubcommandsRegistered(t *testing.T) {
	for _, path := range [][]string{{"settings", "get"}, {"settings", "set"}} {
		cmd := findCmd(path...)
		if cmd == nil {
			t.Errorf("command %v not registered", path)
			continue
		}
		if cmd.Annotations["agent_allowed"] != "true" {
			t.Errorf("command %v agent_allowed = %q, want true", path, cmd.Annotations["agent_allowed"])
		}
		if cmd.Annotations["interactive"] != "" {
			t.Errorf("command %v should not carry interactive annotation", path)
		}
	}
}
