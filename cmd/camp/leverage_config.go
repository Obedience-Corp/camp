package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

var leverageConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "View or update leverage configuration",
	Long: `View or update leverage score configuration settings.

Without flags, displays the current configuration. With flags, updates
the configuration and saves it to .campaign/leverage/config.json.

Configuration parameters:
  --people       Number of developers on the team
  --start        Project start date (YYYY-MM-DD format)
  --cocomo-type  COCOMO project type (organic, semi-detached, embedded)

Examples:
  camp leverage config                         Show current config
  camp leverage config --people 3              Set team size to 3
  camp leverage config --start 2025-01-01      Set project start date
  camp leverage config --people 2 --start 2025-04-28  Set multiple values`,
	RunE: runLeverageConfig,
}

func init() {
	leverageConfigCmd.Flags().Int("people", 0, "number of developers on the team")
	leverageConfigCmd.Flags().String("start", "", "project start date (YYYY-MM-DD)")
	leverageConfigCmd.Flags().String("cocomo-type", "", "COCOMO project type (organic, semi-detached, embedded)")
	leverageCmd.AddCommand(leverageConfigCmd)
}

func runLeverageConfig(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	configPath := leverage.DefaultConfigPath(root)

	// Check if any flags were set
	peopleFlag := cmd.Flags().Lookup("people")
	startFlag := cmd.Flags().Lookup("start")
	cocomoFlag := cmd.Flags().Lookup("cocomo-type")

	hasUpdates := peopleFlag.Changed || startFlag.Changed || cocomoFlag.Changed

	if !hasUpdates {
		return displayLeverageConfig(cmd, ctx, root, configPath)
	}
	return updateLeverageConfig(cmd, configPath, peopleFlag.Changed, startFlag.Changed, cocomoFlag.Changed)
}

func displayLeverageConfig(cmd *cobra.Command, ctx context.Context, root, configPath string) error {
	out := cmd.OutOrStdout()

	// Check if config file exists on disk
	_, statErr := os.Stat(configPath)
	configExists := statErr == nil

	if configExists {
		cfg, err := leverage.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		fmt.Fprintln(out, "Configuration: saved (.campaign/leverage/config.json)")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Team Size:     %d developer(s)\n", cfg.ActualPeople)
		fmt.Fprintf(out, "Project Start: %s\n", cfg.ProjectStart.Format("2006-01-02"))
		fmt.Fprintf(out, "COCOMO Type:   %s\n", cfg.COCOMOProjectType)
		if cfg.AvgWage > 0 {
			fmt.Fprintf(out, "Avg Wage:      $%.0f/year\n", cfg.AvgWage)
		}
	} else {
		// Auto-detect and display
		cfg, err := leverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return fmt.Errorf("auto-detecting config: %w", err)
		}

		fmt.Fprintln(out, "Configuration: auto-detected (no config file found)")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Team Size:     %d developer(s)\n", cfg.ActualPeople)
		if !cfg.ProjectStart.IsZero() {
			fmt.Fprintf(out, "Project Start: %s (earliest git commit)\n", cfg.ProjectStart.Format("2006-01-02"))
		} else {
			fmt.Fprintln(out, "Project Start: unknown (no git history found)")
		}
		fmt.Fprintf(out, "COCOMO Type:   %s\n", cfg.COCOMOProjectType)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Config path:   %s\n", configPath)
	fmt.Fprintln(out, "\nTo update: camp leverage config --people N --start YYYY-MM-DD")

	return nil
}

func updateLeverageConfig(cmd *cobra.Command, configPath string, peopleChanged, startChanged, cocomoChanged bool) error {
	out := cmd.OutOrStdout()

	// Load existing config (returns defaults if file doesn't exist)
	cfg, err := leverage.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply updates
	if peopleChanged {
		people, _ := cmd.Flags().GetInt("people")
		if people <= 0 {
			return fmt.Errorf("people must be greater than 0")
		}
		cfg.ActualPeople = people
	}

	if startChanged {
		startStr, _ := cmd.Flags().GetString("start")
		startDate, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			return fmt.Errorf("invalid date format, use YYYY-MM-DD: %w", err)
		}
		cfg.ProjectStart = startDate
	}

	if cocomoChanged {
		cocomoType, _ := cmd.Flags().GetString("cocomo-type")
		valid := map[string]bool{"organic": true, "semi-detached": true, "embedded": true}
		if !valid[cocomoType] {
			return fmt.Errorf("invalid COCOMO type %q: must be organic, semi-detached, or embedded", cocomoType)
		}
		cfg.COCOMOProjectType = cocomoType
	}

	// Save configuration (SaveConfig creates directories as needed)
	if err := leverage.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintln(out, "Configuration updated successfully")
	fmt.Fprintf(out, "Saved to: %s\n", configPath)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Team Size:     %d developer(s)\n", cfg.ActualPeople)
	fmt.Fprintf(out, "Project Start: %s\n", cfg.ProjectStart.Format("2006-01-02"))
	fmt.Fprintf(out, "COCOMO Type:   %s\n", cfg.COCOMOProjectType)

	return nil
}
