package main

import (
	"context"
	"fmt"
	"os"
	"sort"
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
  --exclude      Exclude a project from leverage scoring
  --include      Include a previously excluded project

Examples:
  camp leverage config                         Show current config
  camp leverage config --people 3              Set team size to 3
  camp leverage config --start 2025-01-01      Set project start date
  camp leverage config --exclude obey-daemon   Exclude a project
  camp leverage config --include obey-daemon   Re-include a project`,
	RunE: runLeverageConfig,
}

func init() {
	leverageConfigCmd.Flags().Int("people", 0, "number of developers on the team")
	leverageConfigCmd.Flags().String("start", "", "project start date (YYYY-MM-DD)")
	leverageConfigCmd.Flags().String("cocomo-type", "", "COCOMO project type (organic, semi-detached, embedded)")
	leverageConfigCmd.Flags().String("exclude", "", "exclude a project from leverage scoring")
	leverageConfigCmd.Flags().String("include", "", "include a previously excluded project")
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
	excludeFlag := cmd.Flags().Lookup("exclude")
	includeFlag := cmd.Flags().Lookup("include")

	hasUpdates := peopleFlag.Changed || startFlag.Changed || cocomoFlag.Changed
	hasProjectUpdate := excludeFlag.Changed || includeFlag.Changed

	if hasProjectUpdate {
		return updateProjectInclusion(cmd, ctx, root, configPath, excludeFlag.Changed, includeFlag.Changed)
	}

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

	var cfg *leverage.LeverageConfig

	if configExists {
		var err error
		cfg, err = leverage.LoadConfig(configPath)
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
		var err error
		cfg, err = leverage.AutoDetectConfig(ctx, root)
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

	// Show project inclusion status
	if len(cfg.Projects) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Projects:")
		names := make([]string, 0, len(cfg.Projects))
		for name := range cfg.Projects {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			entry := cfg.Projects[name]
			status := "included"
			if !entry.Include {
				status = "excluded"
			}
			fmt.Fprintf(out, "  %-20s %s\n", name, status)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Config path:   %s\n", configPath)
	fmt.Fprintln(out, "\nTo update: camp leverage config --people N --start YYYY-MM-DD")

	return nil
}

func updateProjectInclusion(cmd *cobra.Command, ctx context.Context, root, configPath string, excludeChanged, includeChanged bool) error {
	out := cmd.OutOrStdout()

	cfg, err := leverage.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Ensure projects are populated before modifying.
	if len(cfg.Projects) == 0 {
		if err := leverage.PopulateProjects(ctx, root, cfg); err != nil {
			return fmt.Errorf("populating projects: %w", err)
		}
	}

	if excludeChanged {
		name, _ := cmd.Flags().GetString("exclude")
		entry, exists := cfg.Projects[name]
		if !exists {
			return fmt.Errorf("project %q not found in config", name)
		}
		entry.Include = false
		cfg.Projects[name] = entry
		fmt.Fprintf(out, "Excluded project: %s\n", name)
	}

	if includeChanged {
		name, _ := cmd.Flags().GetString("include")
		entry, exists := cfg.Projects[name]
		if !exists {
			return fmt.Errorf("project %q not found in config", name)
		}
		entry.Include = true
		cfg.Projects[name] = entry
		fmt.Fprintf(out, "Included project: %s\n", name)
	}

	if err := leverage.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintf(out, "Saved to: %s\n", configPath)
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
