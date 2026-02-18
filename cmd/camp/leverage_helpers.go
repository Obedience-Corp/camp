package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/leverage"
)

// populateMetrics is the function used to populate project metrics.
// Production code uses leverage.PopulateProjectMetrics (runs blame-weighted PM).
// Tests can replace this with a fast stub to avoid expensive git blame operations.
var populateMetrics func(ctx context.Context, resolved []leverage.ResolvedProject)

// leverageSetup holds common state initialized by all leverage subcommands.
type leverageSetup struct {
	Root          string
	Cfg           *leverage.LeverageConfig
	AutoDetected  bool // true if config was auto-detected (no project_start in file)
	ConfigCreated bool // true if config file was auto-created on first use
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
	configCreated := false
	if len(cfg.Projects) == 0 {
		if err := leverage.PopulateProjects(ctx, root, cfg); err != nil {
			return nil, fmt.Errorf("populating projects: %w", err)
		}

		if err := leverage.SaveConfig(configPath, cfg); err != nil {
			return nil, fmt.Errorf("saving config: %w", err)
		}

		configCreated = !configExists
	}

	return &leverageSetup{Root: root, Cfg: cfg, AutoDetected: autoDetected, ConfigCreated: configCreated}, nil
}

// runPopulateMetrics calls the test-injected populateMetrics or the real
// leverage.PopulateProjectMetrics. Centralizes the dispatch so every command
// uses the same injection point.
func runPopulateMetrics(ctx context.Context, resolved []leverage.ResolvedProject) {
	if populateMetrics != nil {
		populateMetrics(ctx, resolved)
	} else {
		leverage.PopulateProjectMetrics(ctx, resolved)
	}
}

// initRunner returns the SCC runner (test-injected or newly created from config).
func initRunner(cfg *leverage.LeverageConfig) (leverage.Runner, error) {
	if sccRunner != nil {
		return sccRunner, nil
	}
	return leverage.NewSCCRunner(cfg.COCOMOProjectType)
}

// resolveAndPopulateProjects resolves campaign projects, populates git metrics,
// and optionally filters by author. Returns the resolved projects and the count
// of projects excluded by the author filter.
func resolveAndPopulateProjects(ctx context.Context, root string, cfg *leverage.LeverageConfig, authorFilter string) ([]leverage.ResolvedProject, int, error) {
	resolved, err := leverage.ResolveProjects(ctx, root, cfg)
	if err != nil {
		return nil, 0, fmt.Errorf("resolving projects: %w", err)
	}

	runPopulateMetrics(ctx, resolved)

	var authorExcluded int
	if authorFilter != "" {
		var filtered []leverage.ResolvedProject
		for _, proj := range resolved {
			hasCommits, gitErr := leverage.AuthorHasCommits(ctx, proj.GitDir, authorFilter)
			if gitErr == nil && hasCommits {
				filtered = append(filtered, proj)
			} else {
				authorExcluded++
			}
		}
		resolved = filtered
	}

	return resolved, authorExcluded, nil
}

// printVerboseLeverageInfo writes diagnostic config and project resolution details to stderr.
func printVerboseLeverageInfo(cmd *cobra.Command, cfg *leverage.LeverageConfig, setup *leverageSetup, resolved []leverage.ResolvedProject) {
	configPath := leverage.DefaultConfigPath(setup.Root)
	fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Config path: %s\n", configPath)
	fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Auto-detected: %v\n", setup.AutoDetected)
	fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] ActualPeople (config): %d\n", cfg.ActualPeople)
	fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] ProjectStart: %s\n", cfg.ProjectStart.Format("2006-01-02"))

	totalProjects := len(cfg.Projects)
	var excludedNames []string
	for name, entry := range cfg.Projects {
		if !entry.Include {
			excludedNames = append(excludedNames, name)
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Projects in config: %d total, %d excluded\n",
		totalProjects, len(excludedNames))
	for _, n := range excludedNames {
		fmt.Fprintf(cmd.ErrOrStderr(), "[verbose]   excluded: %s\n", n)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Resolved projects: %d\n", len(resolved))
	for _, p := range resolved {
		fmt.Fprintf(cmd.ErrOrStderr(), "[verbose]   %s -> scc:%s git:%s\n", p.Name, p.SCCDir, p.GitDir)
	}
}

// buildScoreRows converts leverage scores into table row data.
func buildScoreRows(scores []*leverage.LeverageScore) [][]string {
	var rows [][]string
	for _, s := range scores {
		estPM := s.EstimatedPeople * s.EstimatedMonths
		authors := "-"
		if s.AuthorCount > 0 {
			authors = fmt.Sprintf("%d", s.AuthorCount)
		}
		actualPM := s.ActualPersonMonths
		if actualPM == 0 {
			actualPM = s.ActualPeople * s.ElapsedMonths
		}
		rows = append(rows, []string{
			s.ProjectName,
			fmtInt(s.TotalFiles),
			fmtInt(s.TotalCode),
			authors,
			"$" + fmtCost(s.EstimatedCost),
			fmtInt(int(estPM)),
			fmt.Sprintf("%.1f", actualPM),
			fmtScore(s.FullLeverage) + "x",
		})
	}
	return rows
}
