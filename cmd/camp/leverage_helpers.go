package main

import (
	"context"
	"fmt"
	"os"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/leverage"
)

// leverageSetup holds common state initialized by all leverage subcommands.
type leverageSetup struct {
	Root         string
	Cfg          *leverage.LeverageConfig
	AutoDetected bool // true if config was auto-detected (no project_start in file)
}

// initLeverageSetup detects the campaign, loads config, and auto-detects if needed.
// On first use (no config file), it auto-creates the config with discovered projects.
// For existing configs with an empty Projects map, it backfills from discovery.
func initLeverageSetup(ctx context.Context) (*leverageSetup, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, fmt.Errorf("not in a campaign: %w", err)
	}

	configPath := leverage.DefaultConfigPath(root)

	_, statErr := os.Stat(configPath)
	configExists := statErr == nil

	cfg, err := leverage.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	autoDetected := cfg.ProjectStart.IsZero()
	if autoDetected {
		detected, err := leverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return nil, fmt.Errorf("auto-detecting config: %w", err)
		}
		cfg = detected
	}

	// Populate projects from discovery if missing.
	if len(cfg.Projects) == 0 {
		if err := leverage.PopulateProjects(ctx, root, cfg); err != nil {
			return nil, fmt.Errorf("populating projects: %w", err)
		}

		if err := leverage.SaveConfig(configPath, cfg); err != nil {
			return nil, fmt.Errorf("saving config: %w", err)
		}

		if !configExists {
			fmt.Println("Created leverage config at .campaign/leverage/config.json")
		}
	}

	return &leverageSetup{Root: root, Cfg: cfg, AutoDetected: autoDetected}, nil
}

// initRunner returns the SCC runner (test-injected or newly created from config).
func initRunner(cfg *leverage.LeverageConfig) (leverage.Runner, error) {
	if sccRunner != nil {
		return sccRunner, nil
	}
	return leverage.NewSCCRunner(cfg.COCOMOProjectType)
}
