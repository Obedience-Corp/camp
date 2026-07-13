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

	// Commit behavior (global defaults + local overrides + effective view).
	settingsKeyGlobalCommitSyncRefs       = "global.commit.sync_project_refs"
	settingsKeyGlobalCommitDisableTags    = "global.commit.disable_commit_tags"
	settingsKeyLocalCommitSyncRefs        = "local.commit.sync_project_refs"
	settingsKeyLocalCommitDisableTags     = "local.commit.disable_commit_tags"
	settingsKeyEffectiveCommitSyncRefs    = "effective.commit.sync_project_refs"
	settingsKeyEffectiveCommitDisableTags = "effective.commit.disable_commit_tags"
)

const settingsThemeInherit = "inherit"

func settingsKeys() []string {
	return []string{
		settingsKeyGlobalTheme,
		settingsKeyGlobalEditor,
		settingsKeyGlobalCampaignsDir,
		settingsKeyGlobalVerbose,
		settingsKeyGlobalNoColor,
		settingsKeyGlobalCommitSyncRefs,
		settingsKeyGlobalCommitDisableTags,
		settingsKeyLocalThemeOverride,
		settingsKeyLocalCommitSyncRefs,
		settingsKeyLocalCommitDisableTags,
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
	Theme        string                `json:"theme"`
	Editor       string                `json:"editor"`
	CampaignsDir string                `json:"campaigns_dir"`
	Verbose      bool                  `json:"verbose"`
	NoColor      bool                  `json:"no_color"`
	Commit       settingsCommitPayload `json:"commit"`
}

type settingsLocalPayload struct {
	ThemeOverride string                 `json:"theme_override"`
	Commit        *settingsCommitPayload `json:"commit,omitempty"`
}

type settingsEffectivePayload struct {
	Theme  string                `json:"theme"`
	Commit settingsCommitPayload `json:"commit"`
}

type settingsCommitPayload struct {
	SyncProjectRefs   bool `json:"sync_project_refs"`
	DisableCommitTags bool `json:"disable_commit_tags"`
}

func commitPayload(p config.CommitPrefs) settingsCommitPayload {
	return settingsCommitPayload{
		SyncProjectRefs:   p.SyncProjectRefs,
		DisableCommitTags: p.DisableCommitTags,
	}
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
  global.commit.sync_project_refs   Default: camp p commit updates campaign-root submodule pointer
  global.commit.disable_commit_tags Default: skip [campaign:…] tags on camp commits
  local.theme_override       Campaign-local theme override (requires a campaign)
  local.commit.sync_project_refs    Campaign override for project-ref sync after project commits
  local.commit.disable_commit_tags  Campaign override to skip commit subject tags
  local.campaign.name        Campaign name in .campaign/campaign.yaml
  local.campaign.description Campaign description
  local.campaign.mission     Campaign mission
  local.campaign.type        Campaign type (product, research, tools, personal)
  local.campaign.commit_hook Commit-message hook command
  effective.commit.*         Resolved commit prefs (get only; local overrides global)

The campaign.yaml list and tree fields (intents.tags, concepts) have no flat
key and are edited only through the interactive 'camp settings' TUI.`,
		Example: `  camp settings get
  camp settings get global.theme
  camp settings get effective.commit.sync_project_refs
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
	rootForEffective := ""
	if inCampaign {
		rootForEffective = root
	}
	if jsonOut {
		return emitSettingsJSON(cmd.OutOrStdout(), cfg, local, inCampaign, effective, rootForEffective)
	}
	return printSettingsAll(cmd.OutOrStdout(), cfg, local, inCampaign, effective, rootForEffective)
}

// resolveSettingValue returns the value for a key, loading campaign.yaml for the
// campaign scalar twins and delegating everything else to settingsValueFor so
// existing keys resolve exactly as before.
func resolveSettingValue(ctx context.Context, key string, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign bool) (any, error) {
	if isCampaignScalarKey(key) {
		return campaignScalarGet(ctx, key)
	}
	switch key {
	case settingsKeyEffectiveCommitSyncRefs, settingsKeyEffectiveCommitDisableTags:
		root := ""
		if inCampaign {
			if r, err := campaign.DetectCached(ctx); err == nil {
				root = r
			}
		}
		prefs := config.EffectiveCommitPrefs(ctx, root)
		if key == settingsKeyEffectiveCommitSyncRefs {
			return prefs.SyncProjectRefs, nil
		}
		return prefs.DisableCommitTags, nil
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
	case settingsKeyGlobalCommitSyncRefs:
		return cfg.Commit.SyncProjectRefs, nil
	case settingsKeyGlobalCommitDisableTags:
		return cfg.Commit.DisableCommitTags, nil
	case settingsKeyLocalCommitSyncRefs:
		if !inCampaign {
			return nil, errSettingsLocalOutsideCampaign(key)
		}
		if local.Commit == nil {
			return false, nil
		}
		return local.Commit.SyncProjectRefs, nil
	case settingsKeyLocalCommitDisableTags:
		if !inCampaign {
			return nil, errSettingsLocalOutsideCampaign(key)
		}
		if local.Commit == nil {
			return false, nil
		}
		return local.Commit.DisableCommitTags, nil
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

func printSettingsAll(w io.Writer, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign bool, effective, campaignRoot string) error {
	lines := []struct {
		key   string
		value any
	}{
		{settingsKeyGlobalTheme, cfg.TUI.Theme},
		{settingsKeyGlobalEditor, cfg.Editor},
		{settingsKeyGlobalCampaignsDir, cfg.CampaignsDir},
		{settingsKeyGlobalVerbose, cfg.Verbose},
		{settingsKeyGlobalNoColor, cfg.NoColor},
		{settingsKeyGlobalCommitSyncRefs, cfg.Commit.SyncProjectRefs},
		{settingsKeyGlobalCommitDisableTags, cfg.Commit.DisableCommitTags},
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
		locSync, locTags := false, false
		if local.Commit != nil {
			locSync, locTags = local.Commit.SyncProjectRefs, local.Commit.DisableCommitTags
		}
		if _, err := fmt.Fprintf(w, "%s = %v\n", settingsKeyLocalCommitSyncRefs, locSync); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s = %v\n", settingsKeyLocalCommitDisableTags, locTags); err != nil {
			return err
		}
	}
	// printSettingsAll is pure formatting; effective prefs are already merged
	// via EffectiveCommitPrefs using campaignRoot (empty when outside a campaign).
	prefs := config.EffectiveCommitPrefs(context.TODO(), campaignRoot)
	if _, err := fmt.Fprintf(w, "%s = %s\n", settingsKeyEffectiveTheme, effective); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s = %v\n", settingsKeyEffectiveCommitSyncRefs, prefs.SyncProjectRefs); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s = %v\n", settingsKeyEffectiveCommitDisableTags, prefs.DisableCommitTags)
	return err
}

func emitSettingsJSON(w io.Writer, cfg *config.GlobalConfig, local *config.LocalSettings, inCampaign bool, effective, campaignRoot string) error {
	prefs := config.EffectiveCommitPrefs(context.TODO(), campaignRoot)
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
			Commit:       commitPayload(cfg.Commit),
		},
		Effective: settingsEffectivePayload{Theme: effective, Commit: commitPayload(prefs)},
	}
	if inCampaign {
		lp := &settingsLocalPayload{ThemeOverride: local.ThemeOverride}
		if local.Commit != nil {
			cp := commitPayload(*local.Commit)
			lp.Commit = &cp
		}
		payload.Local = lp
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func runSettingsSet(ctx context.Context, cmd *cobra.Command, key, value string) error {
	switch {
	case key == settingsKeyGlobalTheme, key == settingsKeyGlobalEditor, key == settingsKeyGlobalCampaignsDir,
		key == settingsKeyGlobalVerbose, key == settingsKeyGlobalNoColor,
		key == settingsKeyGlobalCommitSyncRefs, key == settingsKeyGlobalCommitDisableTags:
		return setGlobalSetting(ctx, cmd, key, value)
	case key == settingsKeyLocalThemeOverride:
		return setLocalThemeOverride(ctx, cmd, value)
	case key == settingsKeyLocalCommitSyncRefs, key == settingsKeyLocalCommitDisableTags:
		return setLocalCommitPref(ctx, cmd, key, value)
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
	case settingsKeyGlobalVerbose, settingsKeyGlobalNoColor,
		settingsKeyGlobalCommitSyncRefs, settingsKeyGlobalCommitDisableTags:
		parsed, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(value)))
		if err != nil {
			return "", camperrors.NewValidation(key,
				fmt.Sprintf("invalid boolean %q (valid: true, false)", value), err)
		}
		switch key {
		case settingsKeyGlobalVerbose:
			cfg.Verbose = parsed
		case settingsKeyGlobalNoColor:
			cfg.NoColor = parsed
		case settingsKeyGlobalCommitSyncRefs:
			cfg.Commit.SyncProjectRefs = parsed
		case settingsKeyGlobalCommitDisableTags:
			cfg.Commit.DisableCommitTags = parsed
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

func setLocalCommitPref(ctx context.Context, cmd *cobra.Command, key, value string) error {
	root, err := campaign.DetectCached(ctx)
	if err != nil || root == "" {
		return errSettingsLocalOutsideCampaign(key)
	}
	parsed, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(value)))
	if err != nil {
		return camperrors.NewValidation(key,
			fmt.Sprintf("invalid boolean %q (valid: true, false)", value), err)
	}
	if err := config.WithLocalSettingsLock(ctx, root, func(s *config.LocalSettings) error {
		base := config.EffectiveCommitPrefs(ctx, root)
		if s.Commit != nil {
			base = *s.Commit
		}
		switch key {
		case settingsKeyLocalCommitSyncRefs:
			base.SyncProjectRefs = parsed
		case settingsKeyLocalCommitDisableTags:
			base.DisableCommitTags = parsed
		}
		cp := base
		s.Commit = &cp
		return nil
	}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", key, strconv.FormatBool(parsed))
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
