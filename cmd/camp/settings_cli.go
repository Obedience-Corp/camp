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

	// campaign.yaml scalar twins of the manifest editor. List and tree fields
	// (intents.tags, concepts) have no flat representation and stay TUI-only.
	settingsKeyLocalCampaignName        = "local.campaign.name"
	settingsKeyLocalCampaignDescription = "local.campaign.description"
	settingsKeyLocalCampaignMission     = "local.campaign.mission"
	settingsKeyLocalCampaignType        = "local.campaign.type"
	settingsKeyLocalCampaignCommitHook  = "local.campaign.commit_hook"
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
		settingsKeyLocalCampaignName,
		settingsKeyLocalCampaignDescription,
		settingsKeyLocalCampaignMission,
		settingsKeyLocalCampaignType,
		settingsKeyLocalCampaignCommitHook,
	}
}

func isCampaignScalarKey(key string) bool {
	switch key {
	case settingsKeyLocalCampaignName, settingsKeyLocalCampaignDescription,
		settingsKeyLocalCampaignMission, settingsKeyLocalCampaignType,
		settingsKeyLocalCampaignCommitHook:
		return true
	default:
		return false
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
  global.theme               Color theme in ~/.obey/campaign/config.json
  global.editor              Preferred editor
  global.campaigns_dir       Where camp create places new campaigns
  global.verbose             Verbose output
  global.no_color            Disable colored output
  local.theme_override       Campaign-local theme override (requires a campaign)
  local.campaign.name        Campaign name in .campaign/campaign.yaml
  local.campaign.description Campaign description
  local.campaign.mission     Campaign mission
  local.campaign.type        Campaign type (product, research, tools, personal)
  local.campaign.commit_hook Commit-message hook command

The campaign.yaml list and tree fields (intents.tags, concepts) have no flat
key and are edited only through the interactive 'camp settings' TUI.`,
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
		return printSettingsValue(ctx, cmd, args[0], cfg, local, inCampaign, jsonOut)
	}
	if jsonOut {
		return emitSettingsJSON(cmd.OutOrStdout(), cfg, local, inCampaign, effective)
	}
	return printSettingsAll(cmd.OutOrStdout(), cfg, local, inCampaign, effective)
}

// resolveSettingValue returns the value for a key, loading campaign.yaml for the
// campaign scalar twins and delegating everything else to settingsValueFor so
// existing keys resolve exactly as before.
func resolveSettingValue(ctx context.Context, key string, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign bool) (any, error) {
	if isCampaignScalarKey(key) {
		return campaignScalarGet(ctx, key)
	}
	return settingsValueFor(key, cfg, local, inCampaign)
}

func campaignScalarGet(ctx context.Context, key string) (any, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil || root == "" {
		return nil, errSettingsLocalOutsideCampaign(key)
	}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "loading campaign.yaml")
	}
	switch key {
	case settingsKeyLocalCampaignName:
		return cfg.Name, nil
	case settingsKeyLocalCampaignDescription:
		return cfg.Description, nil
	case settingsKeyLocalCampaignMission:
		return cfg.Mission, nil
	case settingsKeyLocalCampaignType:
		return string(cfg.Type), nil
	case settingsKeyLocalCampaignCommitHook:
		return cfg.Hooks.CommitMessage.Command, nil
	default:
		return nil, errSettingsUnknownKey(key)
	}
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

func printSettingsValue(ctx context.Context, cmd *cobra.Command, key string, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign, jsonOut bool) error {
	value, err := resolveSettingValue(ctx, key, cfg, local, inCampaign)
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
	switch {
	case key == settingsKeyGlobalTheme, key == settingsKeyGlobalEditor, key == settingsKeyGlobalCampaignsDir,
		key == settingsKeyGlobalVerbose, key == settingsKeyGlobalNoColor:
		return setGlobalSetting(ctx, cmd, key, value)
	case key == settingsKeyLocalThemeOverride:
		return setLocalThemeOverride(ctx, cmd, value)
	case isCampaignScalarKey(key):
		return setCampaignScalar(ctx, cmd, key, value)
	default:
		return errSettingsUnknownKey(key)
	}
}

func setCampaignScalar(ctx context.Context, cmd *cobra.Command, key, value string) error {
	root, err := campaign.DetectCached(ctx)
	if err != nil || root == "" {
		return errSettingsLocalOutsideCampaign(key)
	}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return camperrors.Wrap(err, "loading campaign.yaml")
	}
	display, err := applyCampaignScalarKey(cfg, key, value)
	if err != nil {
		return err
	}
	// Same load-time invariant as SaveCampaignConfig consumers: refuse empty
	// or illegal names before they brick subsequent camp commands.
	if err := config.ValidateCampaignConfig(cfg); err != nil {
		return camperrors.Wrap(err, "invalid campaign.yaml")
	}
	if err := config.SaveCampaignConfig(ctx, root, cfg); err != nil {
		return camperrors.Wrap(err, "saving campaign.yaml")
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", key, display)
	return err
}

// applyCampaignScalarKey sets a single campaign.yaml scalar and returns the
// stored value. type is validated against the known campaign types; the others
// are free text. Only the addressed field changes.
func applyCampaignScalarKey(cfg *config.CampaignConfig, key, value string) (string, error) {
	switch key {
	case settingsKeyLocalCampaignName:
		cfg.Name = strings.TrimSpace(value)
		return cfg.Name, nil
	case settingsKeyLocalCampaignDescription:
		cfg.Description = strings.TrimSpace(value)
		return cfg.Description, nil
	case settingsKeyLocalCampaignMission:
		cfg.Mission = strings.TrimSpace(value)
		return cfg.Mission, nil
	case settingsKeyLocalCampaignType:
		t := config.CampaignType(strings.ToLower(strings.TrimSpace(value)))
		if !t.Valid() {
			return "", camperrors.NewValidation(key,
				fmt.Sprintf("unknown campaign type %q (valid: product, research, tools, personal)", value), nil)
		}
		cfg.Type = t
		return string(t), nil
	case settingsKeyLocalCampaignCommitHook:
		cfg.Hooks.CommitMessage.Command = strings.TrimSpace(value)
		return cfg.Hooks.CommitMessage.Command, nil
	default:
		return "", errSettingsUnknownKey(key)
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
