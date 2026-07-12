package main

import (
	"context"
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/settings"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

const (
	rowSeparator = "─────────────────"
	valSeparator = "_separator"
	valBack      = "back"
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Manage camp configuration",
	Long: `Interactive menu for managing camp configuration.

Global settings live in ~/.obey/campaign/config.json and apply to every
campaign. Local settings live in .campaign/settings/local.json and apply
only to the current campaign; a local theme override wins over the global
theme while you are inside that campaign.

For non-interactive access, use 'camp settings get' and
'camp settings set'. See docs/campaign-settings-files.md in the camp
repository for the file layout.`,
	Example: `  camp settings                              # Interactive settings menu
  camp settings get                          # Print all settings
  camp settings set global.theme dark        # Set the global theme
  camp settings set local.theme_override light`,
	GroupID: "system",
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Fully interactive TUI menu; use 'settings get/set' for automation",
		"interactive":   "true",
	},
	RunE: runSettings,
}

func init() {
	rootCmd.AddCommand(settingsCmd)
	settingsCmd.AddCommand(newSettingsGetCmd())
	settingsCmd.AddCommand(newSettingsSetCmd())
}

func runSettings(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if !ui.IsTerminal() {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "settings requires an interactive terminal")
	}

	_, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	inCampaign := err == nil

	cat, err := settings.BuildCatalog(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "building settings catalog")
	}

	options := []huh.Option[string]{
		huh.NewOption("Global Settings", "global"),
		huh.NewOption("Local Settings (this campaign)", "local"),
		huh.NewOption("Exit", "exit"),
	}

	for {
		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Camp Settings").
				Description("Select configuration scope").
				Options(options...).
				Value(&choice),
		))

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return nil
			}
			return err
		}

		switch choice {
		case "exit", "":
			return nil
		case "global":
			if err := runScopeMenu(ctx, cat, settings.ScopeGlobal, campaignRoot); err != nil {
				return err
			}
		case "local":
			if !inCampaign {
				fmt.Println(ui.Warning("Not inside a campaign. Local settings live in .campaign/settings/local.json; run camp settings from a campaign directory to edit them."))
				continue
			}
			if err := runScopeMenu(ctx, cat, settings.ScopeLocal, campaignRoot); err != nil {
				return err
			}
		}
	}
}

// runScopeMenu renders one scope's sub-menu entirely from the catalog: rows are
// the listable entries for that scope (Hidden/Secret never appear), and each
// selection is dispatched to its editor by entry ID.
func runScopeMenu(ctx context.Context, cat []settings.SettingEntry, scope settings.Scope, campaignRoot string) error {
	title, description := scopeHeader(scope, campaignRoot)
	for {
		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Description(description).
				Options(scopeOptions(cat, scope)...).
				Value(&choice),
		))

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return nil
			}
			return err
		}

		switch choice {
		case valBack:
			return nil
		case valSeparator:
			continue
		default:
			entry, ok := entryByID(cat, choice)
			if !ok {
				continue
			}
			if err := editEntry(ctx, entry, campaignRoot); err != nil {
				return err
			}
		}
	}
}

// scopeOptions builds the selectable rows for a scope from the catalog, followed
// by a separator and a Back row. Each row's value is the entry ID, which the
// router dispatches on.
func scopeOptions(cat []settings.SettingEntry, scope settings.Scope) []huh.Option[string] {
	var options []huh.Option[string]
	for _, e := range settings.ForScope(cat, scope) {
		options = append(options, huh.NewOption(e.Title, e.ID))
	}
	return append(options,
		huh.NewOption(rowSeparator, valSeparator),
		huh.NewOption("Back", valBack),
	)
}

// scopeHeader returns the menu title and a description naming where the scope's
// files live. The precise per-file path is shown on each editor screen.
func scopeHeader(scope settings.Scope, campaignRoot string) (title, description string) {
	switch scope {
	case settings.ScopeGlobal:
		return "Global Settings", "Files under " + pathutil.AbbreviateHome(config.ConfigDir()) + "/"
	case settings.ScopeLocal:
		return "Local Settings (this campaign)", "Files under .campaign/"
	default:
		return "Settings", ""
	}
}

func entryByID(cat []settings.SettingEntry, id string) (settings.SettingEntry, bool) {
	for _, e := range cat {
		if e.ID == id {
			return e, true
		}
	}
	return settings.SettingEntry{}, false
}

// editEntry dispatches a selected catalog entry to its editor by ID. This is the
// single extension point sequences 03-05 plug their editors into. Entries whose
// editor is not implemented yet show a clear message and return to the menu.
func editEntry(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	switch e.ID {
	case "global_config":
		return editGlobalConfig(ctx, e, campaignRoot)
	case "local_settings":
		return editLocalSettingsFile(ctx, e, campaignRoot)
	case "campaign_manifest":
		return editCampaignManifest(ctx, e, campaignRoot)
	case "registry":
		return editRegistry(ctx, e, campaignRoot)
	case "allowlist":
		return editAllowlist(ctx, e, campaignRoot)
	default:
		fmt.Println(ui.Warning(fmt.Sprintf("%s is not editable from the TUI yet.", e.Title)))
		return nil
	}
}

// notYetEditable reports that an entry's structured editor lands in a later
// sequence, naming the exact file so the user can still edit it by hand.
func notYetEditable(e settings.SettingEntry, campaignRoot string) error {
	fmt.Println(ui.Warning(fmt.Sprintf(
		"Editing %s (%s) from the TUI is coming soon.",
		e.Title, settings.CatalogPath(e, campaignRoot))))
	return nil
}

// editCampaignManifest edits .campaign/campaign.yaml (filled in by sequence 03).
func editCampaignManifest(_ context.Context, e settings.SettingEntry, campaignRoot string) error {
	return notYetEditable(e, campaignRoot)
}

// editRegistry edits the global campaign registry (filled in by sequence 04).
func editRegistry(_ context.Context, e settings.SettingEntry, campaignRoot string) error {
	return notYetEditable(e, campaignRoot)
}

// editAllowlist edits .campaign/settings/allowlist.json (filled in by sequence 05).
func editAllowlist(_ context.Context, e settings.SettingEntry, campaignRoot string) error {
	return notYetEditable(e, campaignRoot)
}

// editGlobalConfig opens the global config.json fields as a sub-form under the
// global_config catalog entry. Behavior and persistence are unchanged from the
// former flat global menu.
func editGlobalConfig(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return camperrors.Wrap(err, "loading global config")
	}

	header := "File: " + settings.CatalogPath(e, campaignRoot)
	for {
		options := []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("Theme          %s", displayStr(cfg.TUI.Theme, config.ThemeNameAdaptive)), "theme"),
			huh.NewOption(fmt.Sprintf("Editor         %s", displayStr(cfg.Editor, "$EDITOR")), "editor"),
			huh.NewOption(fmt.Sprintf("Campaigns Dir  %s", displayStr(cfg.CampaignsDir, "~/campaigns")), "campaigns_dir"),
			huh.NewOption(fmt.Sprintf("Verbose        %s", boolStr(cfg.Verbose)), "verbose"),
			huh.NewOption(fmt.Sprintf("No Color       %s", boolStr(cfg.NoColor)), "no_color"),
			huh.NewOption(rowSeparator, valSeparator),
			huh.NewOption("Back", valBack),
		}

		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(e.Title).
				Description(header).
				Options(options...).
				Value(&choice),
		))

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return nil
			}
			return err
		}

		switch choice {
		case valBack:
			return nil
		case valSeparator:
			continue
		case "theme":
			if err := editTheme(ctx, cfg); err != nil {
				return err
			}
		case "editor":
			if err := editEditor(ctx, cfg); err != nil {
				return err
			}
		case "campaigns_dir":
			if err := editCampaignsDir(ctx, cfg); err != nil {
				return err
			}
		case "verbose":
			cfg.Verbose = !cfg.Verbose
			if err := config.SaveGlobalConfig(ctx, cfg); err != nil {
				return camperrors.Wrap(err, "saving config")
			}
		case "no_color":
			cfg.NoColor = !cfg.NoColor
			if err := config.SaveGlobalConfig(ctx, cfg); err != nil {
				return camperrors.Wrap(err, "saving config")
			}
		}
	}
}

// editLocalSettingsFile opens the local.json fields (theme override) as a
// sub-form under the local_settings catalog entry. Behavior and persistence are
// unchanged from the former flat local menu.
func editLocalSettingsFile(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	header := "File: " + settings.CatalogPath(e, campaignRoot)
	for {
		local, err := config.LoadLocalSettings(ctx, campaignRoot)
		if err != nil {
			return camperrors.Wrap(err, "loading local settings")
		}

		options := []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("Theme Override  %s (effective: %s)",
				displayStr(local.ThemeOverride, "inherit global"),
				config.EffectiveTheme(ctx)), "theme_override"),
			huh.NewOption(rowSeparator, valSeparator),
			huh.NewOption("Back", valBack),
		}

		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(e.Title).
				Description(header).
				Options(options...).
				Value(&choice),
		))

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return nil
			}
			return err
		}

		switch choice {
		case valBack:
			return nil
		case valSeparator:
			continue
		case "theme_override":
			if err := editLocalThemeOverride(ctx, campaignRoot, local.ThemeOverride); err != nil {
				return err
			}
		}
	}
}

func editLocalThemeOverride(ctx context.Context, campaignRoot, current string) error {
	value := current
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Theme Override").
			Description("Campaign-local theme; overrides the global theme in this campaign").
			Options(
				huh.NewOption("Inherit global", ""),
				huh.NewOption("Adaptive - Auto-detect", config.ThemeNameAdaptive),
				huh.NewOption("Light - For light backgrounds", config.ThemeNameLight),
				huh.NewOption("Dark - For dark backgrounds", config.ThemeNameDark),
				huh.NewOption("High Contrast - Maximum visibility", config.ThemeNameHighContrast),
			).
			Value(&value),
	))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return err
	}

	return config.WithLocalSettingsLock(ctx, campaignRoot, func(s *config.LocalSettings) error {
		s.ThemeOverride = value
		return nil
	})
}

func editTheme(ctx context.Context, cfg *config.GlobalConfig) error {
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Theme").
			Description("Color theme for TUI elements").
			Options(
				huh.NewOption("Adaptive - Auto-detect", config.ThemeNameAdaptive),
				huh.NewOption("Light - For light backgrounds", config.ThemeNameLight),
				huh.NewOption("Dark - For dark backgrounds", config.ThemeNameDark),
				huh.NewOption("High Contrast - Maximum visibility", config.ThemeNameHighContrast),
			).
			Value(&cfg.TUI.Theme),
	))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return err
	}

	return config.SaveGlobalConfig(ctx, cfg)
}

func editEditor(ctx context.Context, cfg *config.GlobalConfig) error {
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Editor").
			Description("Preferred editor (leave empty for $EDITOR or vim)").
			Value(&cfg.Editor),
	))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return err
	}

	return config.SaveGlobalConfig(ctx, cfg)
}

func editCampaignsDir(ctx context.Context, cfg *config.GlobalConfig) error {
	value := cfg.CampaignsDir
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Campaigns Dir").
				Description("Where 'camp create' places new campaigns. Leave empty for default (~/campaigns).").
				Value(&value),
		),
	)
	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return camperrors.Wrap(err, "editing campaigns_dir")
	}
	if err := applyCampaignsDirCandidate(cfg, value); err != nil {
		// Surface the error and return without saving; user can re-edit on the next loop iteration.
		fmt.Println(ui.Warning(fmt.Sprintf("Invalid value: %v", err)))
		return nil
	}
	if err := config.SaveGlobalConfig(ctx, cfg); err != nil {
		return camperrors.Wrap(err, "saving config")
	}
	return nil
}

func applyCampaignsDirCandidate(cfg *config.GlobalConfig, value string) error {
	next := *cfg
	next.CampaignsDir = strings.TrimSpace(value) // empty input clears back to default
	if err := config.ValidateGlobalConfig(&next); err != nil {
		return err
	}
	cfg.CampaignsDir = next.CampaignsDir
	return nil
}

// Helper functions
func displayStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func boolStr(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
