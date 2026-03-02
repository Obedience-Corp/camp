package main

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/leverage"
)

// populateMetrics is the function used to populate project metrics.
// Production code uses the three-tier blame cache (runs blame-weighted PM).
// Tests can replace this with a fast stub to avoid expensive git blame operations.
var populateMetrics func(ctx context.Context, campaignRoot string, resolved []leverage.ResolvedProject, resolver *leverage.AuthorResolver)

// leverageSetup holds common state initialized by all leverage subcommands.
type leverageSetup struct {
	Root          string
	Cfg           *leverage.LeverageConfig
	Resolver      *leverage.AuthorResolver
	AutoDetected  bool // true if config was auto-detected (no project_start in file)
	ConfigCreated bool // true if config file was auto-created on first use
}

// initLeverageSetup detects the campaign, loads config, and auto-detects if needed.
// On first use (no config file), it auto-creates the config with discovered projects.
// For existing configs with an empty Projects map, it backfills from discovery.
func initLeverageSetup(ctx context.Context) (*leverageSetup, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "not in a campaign")
	}

	configPath := leverage.DefaultConfigPath(root)

	_, statErr := os.Stat(configPath)
	configExists := statErr == nil

	cfg, err := leverage.LoadConfig(configPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "loading config")
	}

	autoDetected := cfg.ProjectStart.IsZero()
	if autoDetected {
		detected, err := leverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return nil, camperrors.Wrap(err, "auto-detecting config")
		}
		cfg = detected
	}

	// Sync projects from discovery: adds new projects, removes stale entries.
	if err := leverage.PopulateProjects(ctx, root, cfg); err != nil {
		return nil, camperrors.Wrap(err, "populating projects")
	}

	if err := leverage.SaveConfig(configPath, cfg); err != nil {
		return nil, camperrors.Wrap(err, "saving config")
	}

	configCreated := !configExists

	// Load or auto-generate authors.json for canonical author resolution.
	authorsPath := leverage.DefaultAuthorsPath(root)
	authorCfg, err := leverage.LoadAuthorConfig(authorsPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "loading authors config")
	}

	// Resolve projects to collect git dirs for author auto-detection.
	// ResolveProjects is lightweight (path resolution only, no git/scc).
	resolved, resolveErr := leverage.ResolveProjects(ctx, root, cfg)
	if resolveErr == nil && len(resolved) > 0 {
		gitDirSet := make(map[string]bool, len(resolved))
		for _, p := range resolved {
			gitDirSet[p.GitDir] = true
		}
		gitDirs := make([]string, 0, len(gitDirSet))
		for dir := range gitDirSet {
			gitDirs = append(gitDirs, dir)
		}

		if authorCfg == nil {
			// First run: auto-detect identity groups and save.
			detected, detectErr := leverage.AutoDetectAuthors(ctx, gitDirs)
			if detectErr == nil && len(detected.Authors) > 0 {
				authorCfg = detected
				_ = leverage.SaveAuthorConfig(authorsPath, authorCfg)
				fmt.Fprintf(os.Stderr, "Created authors config at .campaign/leverage/authors.json (%d authors)\n", len(authorCfg.Authors))
			}
		} else {
			// Subsequent run: sync newly discovered emails into existing config.
			detected, detectErr := leverage.AutoDetectAuthors(ctx, gitDirs)
			if detectErr == nil {
				if leverage.SyncAuthors(authorCfg, detected) {
					_ = leverage.SaveAuthorConfig(authorsPath, authorCfg)
				}
			}
		}
	}

	resolver := leverage.NewAuthorResolver(authorCfg)

	return &leverageSetup{Root: root, Cfg: cfg, Resolver: resolver, AutoDetected: autoDetected, ConfigCreated: configCreated}, nil
}

// runPopulateMetrics uses three-tier blame caching for fast repeat runs:
//   - Tier A: Exact hash match → populate from cache (<1ms)
//   - Tier B: Hash differs, cache exists → incremental update (re-blame changed files)
//   - Tier C: No cache → full compute and save
//
// Falls back to the test-injected populateMetrics if set.
func runPopulateMetrics(ctx context.Context, campaignRoot string, resolved []leverage.ResolvedProject, resolver *leverage.AuthorResolver, verbose bool) {
	if populateMetrics != nil {
		populateMetrics(ctx, campaignRoot, resolved, resolver)
		return
	}

	cache := leverage.NewBlameCache(leverage.DefaultCacheDir(campaignRoot))
	total := len(resolved)
	var cached, incremental, full int

	for i := range resolved {
		if err := ctx.Err(); err != nil {
			return
		}

		p := &resolved[i]

		hash, err := leverage.ProjectHash(ctx, p)
		if err != nil {
			// Can't determine hash; fall back to full compute.
			fmt.Fprintf(os.Stderr, "  Analyzing %s (%d/%d)...\n", p.Name, i+1, total)
			leverage.PopulateOneProject(ctx, p, resolver)
			full++
			continue
		}

		entry, _ := cache.Load(ctx, p.Name)

		switch {
		case entry != nil && entry.CommitHash == hash && entry.SCCDir == p.SCCDir:
			// Tier A: exact match.
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s (%d/%d) cached\n", p.Name, i+1, total)
			}
			// Always recompute author count (fast git shortlog call) so
			// deduplication improvements take effect without cache busting.
			if count, err := leverage.CountAuthors(ctx, p.GitDir, resolver); err == nil {
				p.AuthorCount = count
			} else {
				p.AuthorCount = entry.AuthorCount
			}
			p.ActualPersonMonths = entry.ActualPM
			p.Authors = entry.Authors
			cached++

		case entry != nil && entry.FileBlame != nil:
			// Tier B: incremental update.
			subpath := ""
			if p.InMonorepo {
				rel, relErr := leverage.RelPath(p.GitDir, p.SCCDir)
				if relErr == nil {
					subpath = rel
				}
			}

			modified, added, deleted, diffErr := leverage.ChangedFiles(ctx, p.GitDir, entry.CommitHash, hash, subpath)
			if diffErr != nil {
				// Fall back to full compute.
				fmt.Fprintf(os.Stderr, "  Analyzing %s (%d/%d)...\n", p.Name, i+1, total)
				leverage.PopulateOneProjectCached(ctx, p, cache, hash, resolver)
				full++
				continue
			}

			totalChanged := len(modified) + len(added) + len(deleted)
			fmt.Fprintf(os.Stderr, "  %s (%d/%d) updating %d files...\n", p.Name, i+1, total, totalChanged)

			if err := entry.IncrementalUpdate(ctx, p.SCCDir, modified, added, deleted); err != nil {
				// Fall back to full compute.
				leverage.PopulateOneProjectCached(ctx, p, cache, hash, resolver)
				full++
				continue
			}

			entry.RecomputeAggregates()
			entry.CommitHash = hash
			entry.SCCDir = p.SCCDir

			leverage.RecomputeProjectMetrics(ctx, p, entry, resolver)

			_ = cache.Save(ctx, entry)
			incremental++

		default:
			// Tier C: full compute.
			fmt.Fprintf(os.Stderr, "  Analyzing %s (%d/%d)...\n", p.Name, i+1, total)
			leverage.PopulateOneProjectCached(ctx, p, cache, hash, resolver)
			full++
		}
	}

	if verbose || incremental > 0 || full > 0 {
		fmt.Fprintf(os.Stderr, "  Blame: %d cached, %d incremental, %d full\n", cached, incremental, full)
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
func resolveAndPopulateProjects(ctx context.Context, root string, cfg *leverage.LeverageConfig, resolver *leverage.AuthorResolver, authorFilter string, verbose bool) ([]leverage.ResolvedProject, int, error) {
	resolved, err := leverage.ResolveProjects(ctx, root, cfg)
	if err != nil {
		return nil, 0, camperrors.Wrap(err, "resolving projects")
	}

	runPopulateMetrics(ctx, root, resolved, resolver, verbose)

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
