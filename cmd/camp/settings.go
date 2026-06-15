package main

import (
	"context"
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
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
			if err := runGlobalSettings(ctx); err != nil {
				return err
			}
		case "local":
			if !inCampaign {
				fmt.Println(ui.Warning("Not inside a campaign. Local settings live in .campaign/settings/local.json; run camp settings from a campaign directory to edit them."))
				continue
			}
			if err := runLocalSettings(ctx, campaignRoot); err != nil {
				return err
			}
		}
	}
}

func runGlobalSettings(ctx context.Context) error {
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return camperrors.Wrap(err, "loading global config")
	}

	for {
		options := []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("Theme          %s", displayStr(cfg.TUI.Theme, config.ThemeNameAdaptive)), "theme"),
			huh.NewOption(fmt.Sprintf("Editor         %s", displayStr(cfg.Editor, "$EDITOR")), "editor"),
			huh.NewOption(fmt.Sprintf("Campaigns Dir  %s", displayStr(cfg.CampaignsDir, "~/campaigns")), "campaigns_dir"),
			huh.NewOption(fmt.Sprintf("Verbose        %s", boolStr(cfg.Verbose)), "verbose"),
			huh.NewOption(fmt.Sprintf("No Color       %s", boolStr(cfg.NoColor)), "no_color"),
			huh.NewOption("─────────────────", "_separator"),
			huh.NewOption("Back", "back"),
		}

		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Global Settings").
				Description("Changes apply to all campaigns").
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
		case "back":
			return nil
		case "_separator":
			// Ignore separator selection, continue the loop
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

func runLocalSettings(ctx context.Context, campaignRoot string) error {
	for {
		local, err := config.LoadLocalSettings(ctx, campaignRoot)
		if err != nil {
			return camperrors.Wrap(err, "loading local settings")
		}

		options := []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("Theme Override  %s (effective: %s)",
				displayStr(local.ThemeOverride, "inherit global"),
				config.EffectiveTheme(ctx)), "theme_override"),
			huh.NewOption("─────────────────", "_separator"),
			huh.NewOption("Back", "back"),
		}

		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Local Settings (Campaign-Specific)").
				Description("Changes apply only to this campaign").
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
		case "back":
			return nil
		case "_separator":
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
