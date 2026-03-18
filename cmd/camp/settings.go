package main

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Manage camp configuration",
	Long: `Interactive menu for managing camp configuration.

Today, this command edits global user preferences in
~/.obey/campaign/config.json.

Campaign-local settings still live in files under .campaign/, and the
"Local Settings" menu is currently a scaffold rather than a full editor.
See docs/campaign-settings-files.md in the camp repository for the current
file layout.`,
	Example: `  camp settings   # Edit global editor/theme preferences`,
	GroupID: "system",
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Fully interactive TUI menu",
		"interactive":   "true",
	},
	RunE: runSettings,
}

func init() {
	rootCmd.AddCommand(settingsCmd)
}

func runSettings(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Check if we're in a campaign
	_, _, err := config.LoadCampaignConfigFromCwd(ctx)
	inCampaign := err == nil

	// Build options
	options := []huh.Option[string]{
		huh.NewOption("Global Settings", "global"),
	}
	if inCampaign {
		options = append(options, huh.NewOption("Local Settings (this campaign)", "local"))
	}
	options = append(options, huh.NewOption("Exit", "exit"))

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
			if err := runLocalSettings(ctx); err != nil {
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
			huh.NewOption(fmt.Sprintf("Theme          %s", displayStr(cfg.TUI.Theme, "adaptive")), "theme"),
			huh.NewOption(fmt.Sprintf("Editor         %s", displayStr(cfg.Editor, "$EDITOR")), "editor"),
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

// runLocalSettings shows a mock/scaffold menu for local campaign settings.
// TODO: Implement actual local config loading/saving when local settings are needed.
// This scaffold exists to establish the navigation structure for future implementation.
func runLocalSettings(ctx context.Context) error {
	for {
		// MOCK: These are placeholder settings showing the intended structure.
		options := []huh.Option[string]{
			huh.NewOption("[MOCK] Theme Override       NOT IMPLEMENTED", "theme"),
			huh.NewOption("[MOCK] Project Defaults     NOT IMPLEMENTED", "projects"),
			huh.NewOption("[MOCK] Intent Defaults      NOT IMPLEMENTED", "intents"),
			huh.NewOption("─────────────────────────────────────────", "_separator"),
			huh.NewOption("Back", "back"),
		}

		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Local Settings (Campaign-Specific)").
				Description("Local settings are not yet implemented - this is a UI scaffold").
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
		default:
			fmt.Println("\nThis setting is not yet implemented.")
			fmt.Println("Local campaign settings will be added in a future update.")
		}
	}
}

func editTheme(ctx context.Context, cfg *config.GlobalConfig) error {
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Theme").
			Description("Color theme for TUI elements").
			Options(
				huh.NewOption("Adaptive - Auto-detect", "adaptive"),
				huh.NewOption("Light - For light backgrounds", "light"),
				huh.NewOption("Dark - For dark backgrounds", "dark"),
				huh.NewOption("High Contrast - Maximum visibility", "high-contrast"),
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
