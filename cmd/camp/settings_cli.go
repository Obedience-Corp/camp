package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

// SettingsJSONVersion is the schema version of camp settings get --json.
//
// Changelog:
//   - v1alpha1: initial payload (global theme/editor/campaigns_dir/verbose/
//     no_color, local theme_override when inside a campaign, effective theme,
//     in_campaign). Single-key form emits {schema_version, generated_at, key,
//     value}.
const SettingsJSONVersion = "settings/v1alpha1"

const (
	settingsKeyGlobalTheme        = "global.theme"
	settingsKeyGlobalEditor       = "global.editor"
	settingsKeyGlobalCampaignsDir = "global.campaigns_dir"
	settingsKeyGlobalVerbose      = "global.verbose"
	settingsKeyGlobalNoColor      = "global.no_color"
	settingsKeyLocalThemeOverride = "local.theme_override"
	settingsKeyEffectiveTheme     = "effective.theme"
)

const settingsThemeInherit = "inherit"

func settingsKeys() []string {
	return []string{
		settingsKeyGlobalTheme,
		settingsKeyGlobalEditor,
		settingsKeyGlobalCampaignsDir,
		settingsKeyGlobalVerbose,
		settingsKeyGlobalNoColor,
		settingsKeyLocalThemeOverride,
	}
}

type settingsPayload struct {
	SchemaVersion string                   `json:"schema_version"`
	GeneratedAt   time.Time                `json:"generated_at"`
	InCampaign    bool                     `json:"in_campaign"`
	Global        settingsGlobalPayload    `json:"global"`
	Local         *settingsLocalPayload    `json:"local,omitempty"`
	Effective     settingsEffectivePayload `json:"effective"`
}

type settingsGlobalPayload struct {
	Theme        string `json:"theme"`
	Editor       string `json:"editor"`
	CampaignsDir string `json:"campaigns_dir"`
	Verbose      bool   `json:"verbose"`
	NoColor      bool   `json:"no_color"`
}

type settingsLocalPayload struct {
	ThemeOverride string `json:"theme_override"`
}

type settingsEffectivePayload struct {
	Theme string `json:"theme"`
}

type settingsValuePayload struct {
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Key           string    `json:"key"`
	Value         any       `json:"value"`
}

func newSettingsGetCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "get [key]",
		Short: "Print camp settings",
		Long: `Print camp settings non-interactively.

With no key, prints all settings including the effective theme. With a key,
prints just that value.

Keys:
  global.theme           Color theme in ~/.obey/campaign/config.json
  global.editor          Preferred editor
  global.campaigns_dir   Where camp create places new campaigns
  global.verbose         Verbose output
  global.no_color        Disable colored output
  local.theme_override   Campaign-local theme override (requires a campaign)`,
		Example: `  camp settings get
  camp settings get global.theme
  camp settings get --json`,
		Args:         jsoncontract.Args(SettingsJSONVersion, func() bool { return jsonOut }, cobra.MaximumNArgs(1)),
		SilenceUsage: true,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only settings output with --json support",
		},
		RunE: jsoncontract.RunE(SettingsJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runSettingsGet(cmd.Context(), cmd, args, jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(SettingsJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func newSettingsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a camp setting",
		Long: `Set a camp setting non-interactively.

Accepts the same keys as 'camp settings get'. Theme values are one of
adaptive, light, dark, or high-contrast. Boolean values accept true/false.
Setting local.theme_override to 'inherit' clears the override; local.* keys
require running inside a campaign.`,
		Example: `  camp settings set global.theme dark
  camp settings set global.verbose true
  camp settings set local.theme_override light
  camp settings set local.theme_override inherit`,
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Non-interactive settings mutation with validated values",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsSet(cmd.Context(), cmd, args[0], args[1])
		},
	}
}

func runSettingsGet(ctx context.Context, cmd *cobra.Command, args []string, jsonOut bool) error {
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return camperrors.Wrap(err, "loading global config")
	}

	root, rootErr := campaign.DetectCached(ctx)
	inCampaign := rootErr == nil && root != ""

	var local *config.LocalSettings
	if inCampaign {
		local, err = config.LoadLocalSettings(ctx, root)
		if err != nil {
			return err
		}
	}

	effective := config.EffectiveThemeFrom(cfg, local)

	if len(args) == 1 {
		return printSettingsValue(cmd, args[0], cfg, local, inCampaign, jsonOut)
	}
	if jsonOut {
		return emitSettingsJSON(cmd.OutOrStdout(), cfg, local, inCampaign, effective)
	}
	return printSettingsAll(cmd.OutOrStdout(), cfg, local, inCampaign, effective)
}

func settingsValueFor(key string, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign bool) (any, error) {
	switch key {
	case settingsKeyGlobalTheme:
		return cfg.TUI.Theme, nil
	case settingsKeyGlobalEditor:
		return cfg.Editor, nil
	case settingsKeyGlobalCampaignsDir:
		return cfg.CampaignsDir, nil
	case settingsKeyGlobalVerbose:
		return cfg.Verbose, nil
	case settingsKeyGlobalNoColor:
		return cfg.NoColor, nil
	case settingsKeyLocalThemeOverride:
		if !inCampaign {
			return nil, errSettingsLocalOutsideCampaign(key)
		}
		return local.ThemeOverride, nil
	default:
		return nil, errSettingsUnknownKey(key)
	}
}

func printSettingsValue(cmd *cobra.Command, key string, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign, jsonOut bool) error {
	value, err := settingsValueFor(key, cfg, local, inCampaign)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(settingsValuePayload{
			SchemaVersion: SettingsJSONVersion,
			GeneratedAt:   time.Now().UTC(),
			Key:           key,
			Value:         value,
		})
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%v\n", value)
	return err
}

func printSettingsAll(w io.Writer, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign bool, effective string) error {
	lines := []struct {
		key   string
		value any
	}{
		{settingsKeyGlobalTheme, cfg.TUI.Theme},
		{settingsKeyGlobalEditor, cfg.Editor},
		{settingsKeyGlobalCampaignsDir, cfg.CampaignsDir},
		{settingsKeyGlobalVerbose, cfg.Verbose},
		{settingsKeyGlobalNoColor, cfg.NoColor},
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s = %v\n", line.key, line.value); err != nil {
			return err
		}
	}
	if inCampaign {
		if _, err := fmt.Fprintf(w, "%s = %s\n", settingsKeyLocalThemeOverride, local.ThemeOverride); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%s = %s\n", settingsKeyEffectiveTheme, effective)
	return err
}

func emitSettingsJSON(w io.Writer, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign bool, effective string) error {
	payload := settingsPayload{
		SchemaVersion: SettingsJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		InCampaign:    inCampaign,
		Global: settingsGlobalPayload{
			Theme:        cfg.TUI.Theme,
			Editor:       cfg.Editor,
			CampaignsDir: cfg.CampaignsDir,
			Verbose:      cfg.Verbose,
			NoColor:      cfg.NoColor,
		},
		Effective: settingsEffectivePayload{Theme: effective},
	}
	if inCampaign {
		payload.Local = &settingsLocalPayload{ThemeOverride: local.ThemeOverride}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func runSettingsSet(ctx context.Context, cmd *cobra.Command, key, value string) error {
	switch key {
	case settingsKeyGlobalTheme, settingsKeyGlobalEditor, settingsKeyGlobalCampaignsDir,
		settingsKeyGlobalVerbose, settingsKeyGlobalNoColor:
		return setGlobalSetting(ctx, cmd, key, value)
	case settingsKeyLocalThemeOverride:
		return setLocalThemeOverride(ctx, cmd, value)
	default:
		return errSettingsUnknownKey(key)
	}
}

func setGlobalSetting(ctx context.Context, cmd *cobra.Command, key, value string) error {
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return camperrors.Wrap(err, "loading global config")
	}

	display, err := applyGlobalSetting(cfg, key, value)
	if err != nil {
		return err
	}

	if err := config.SaveGlobalConfig(ctx, cfg); err != nil {
		return camperrors.Wrap(err, "saving global config")
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", key, display)
	return err
}

func applyGlobalSetting(cfg *config.GlobalConfig, key, value string) (string, error) {
	switch key {
	case settingsKeyGlobalTheme:
		themeName := strings.ToLower(strings.TrimSpace(value))
		if !config.IsValidThemeName(themeName) {
			return "", camperrors.NewValidation(key,
				fmt.Sprintf("unknown theme %q (valid: %s)", value, strings.Join(config.ValidThemeNames(), ", ")), nil)
		}
		cfg.TUI.Theme = themeName
		return themeName, nil
	case settingsKeyGlobalEditor:
		cfg.Editor = strings.TrimSpace(value)
		return cfg.Editor, nil
	case settingsKeyGlobalCampaignsDir:
		next := *cfg
		next.CampaignsDir = strings.TrimSpace(value)
		if err := config.ValidateGlobalConfig(&next); err != nil {
			return "", camperrors.NewValidation(key, err.Error(), err)
		}
		cfg.CampaignsDir = next.CampaignsDir
		return cfg.CampaignsDir, nil
	case settingsKeyGlobalVerbose, settingsKeyGlobalNoColor:
		parsed, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(value)))
		if err != nil {
			return "", camperrors.NewValidation(key,
				fmt.Sprintf("invalid boolean %q (valid: true, false)", value), err)
		}
		if key == settingsKeyGlobalVerbose {
			cfg.Verbose = parsed
		} else {
			cfg.NoColor = parsed
		}
		return strconv.FormatBool(parsed), nil
	default:
		return "", errSettingsUnknownKey(key)
	}
}

func setLocalThemeOverride(ctx context.Context, cmd *cobra.Command, value string) error {
	root, err := campaign.DetectCached(ctx)
	if err != nil || root == "" {
		return errSettingsLocalOutsideCampaign(settingsKeyLocalThemeOverride)
	}

	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == settingsThemeInherit {
		normalized = ""
	}
	if normalized != "" && !config.IsValidThemeName(normalized) {
		return camperrors.NewValidation(settingsKeyLocalThemeOverride,
			fmt.Sprintf("unknown theme %q (valid: %s, %s)", value,
				strings.Join(config.ValidThemeNames(), ", "), settingsThemeInherit), nil)
	}

	if err := config.WithLocalSettingsLock(ctx, root, func(s *config.LocalSettings) error {
		s.ThemeOverride = normalized
		return nil
	}); err != nil {
		return err
	}

	if normalized == "" {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "cleared %s\n", settingsKeyLocalThemeOverride)
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", settingsKeyLocalThemeOverride, normalized)
	return err
}

func errSettingsUnknownKey(key string) error {
	return camperrors.NewValidation("key",
		fmt.Sprintf("unknown settings key %q (valid: %s)", key, strings.Join(settingsKeys(), ", ")), nil)
}

func errSettingsLocalOutsideCampaign(key string) error {
	return camperrors.NewValidation(key,
		"local settings require a campaign; run this command inside a campaign directory", nil)
}
