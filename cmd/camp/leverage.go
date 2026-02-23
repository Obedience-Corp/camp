package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/leverage"
)

// sccRunner is the package-level runner used by the leverage command.
// Tests can replace this to inject a mock.
var sccRunner leverage.Runner

var leverageCmd = &cobra.Command{
	Use:   "leverage",
	Short: "Compute leverage scores for campaign projects",
	Long: `Compute productivity leverage scores by comparing scc COCOMO estimates
against actual development effort.

Leverage score measures how much more output you produce versus what
traditional estimation models predict for the same team and time.

  FullLeverage   = (EstimatedPeople x EstimatedMonths) / (ActualPeople x ElapsedMonths)
  SimpleLeverage = EstimatedPeople / ActualPeople

Examples:
  camp leverage                              Show team leverage (auto-detect authors from git)
  camp leverage --author lance@example.com   Show personal leverage
  camp leverage --project camp               Show score for specific project
  camp leverage --json                       Output as JSON
  camp leverage --people 2                   Override team size
  camp leverage --verbose                    Show diagnostic details`,
	RunE: runLeverage,
}

func init() {
	leverageCmd.Flags().Bool("json", false, "output as JSON")
	leverageCmd.Flags().StringP("project", "p", "", "filter by project name")
	leverageCmd.Flags().Int("people", 0, "override team size (0 = auto-detect from git)")
	leverageCmd.Flags().Bool("no-legend", false, "hide the leverage formula legend")
	leverageCmd.Flags().BoolP("verbose", "v", false, "show diagnostic details (config, project resolution, exclusions)")
	leverageCmd.Flags().String("author", "", "filter by author email (git substring match — 'alice@co' matches 'alice@co.com')")
	leverageCmd.Flags().Bool("by-author", false, "show per-author leverage breakdown")
	rootCmd.AddCommand(leverageCmd)
	leverageCmd.GroupID = "campaign"
}

func runLeverage(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	jsonOut, _ := cmd.Flags().GetBool("json")
	projectFilter, _ := cmd.Flags().GetString("project")
	peopleOverride, _ := cmd.Flags().GetInt("people")
	verbose, _ := cmd.Flags().GetBool("verbose")
	authorFilter, _ := cmd.Flags().GetString("author")
	byAuthor, _ := cmd.Flags().GetBool("by-author")

	setup, err := initLeverageSetup(ctx)
	if err != nil {
		return err
	}
	if setup.ConfigCreated {
		fmt.Fprintln(cmd.OutOrStdout(), "Created leverage config at .campaign/leverage/config.json")
	}
	cfg := setup.Cfg

	// Apply people override if specified
	if peopleOverride > 0 {
		cfg.ActualPeople = peopleOverride
	}

	// Default author from config if --author not set
	if authorFilter == "" && cfg.AuthorEmail != "" {
		authorFilter = cfg.AuthorEmail
	}

	runner, err := initRunner(cfg)
	if err != nil {
		return err
	}

	resolved, authorExcluded, err := resolveAndPopulateProjects(ctx, setup.Root, cfg, setup.Resolver, authorFilter, verbose)
	if err != nil {
		return err
	}

	// Verbose: show config and project resolution details
	if verbose {
		printVerboseLeverageInfo(cmd, cfg, setup, resolved)
	}

	// Compute elapsed months
	now := time.Now()
	elapsed := leverage.ElapsedMonths(cfg.ProjectStart, now)

	// Run scc and compute scores for each project
	var scores []*leverage.LeverageScore
	for _, proj := range resolved {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if projectFilter != "" && proj.Name != projectFilter {
			continue
		}

		result, err := runner.Run(ctx, proj.SCCDir, proj.ExcludeDirs)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s: %v\n", proj.Name, err)
			continue
		}

		// Determine actual person-months for this project.
		// Use contribution-based actual PM (sum of each author's active duration)
		// rather than naive numAuthors * totalElapsed.
		var projActualPM float64
		var projPeople int
		var projElapsed float64

		if authorFilter != "" {
			// Personal leverage: 1 person, author-specific date range
			projPeople = 1
			first, last, gitErr := leverage.AuthorDateRange(ctx, proj.GitDir, authorFilter)
			if gitErr == nil {
				projElapsed = leverage.ElapsedMonths(first, last)
			}
			if projElapsed <= 0 {
				projElapsed = 0.1 // minimum for single-commit authors
			}
			projActualPM = projElapsed
		} else if peopleOverride > 0 {
			// Manual override: use specified people count with project elapsed
			projPeople = peopleOverride
			first, last, gitErr := leverage.GitDateRange(ctx, proj.GitDir)
			if gitErr == nil {
				projElapsed = leverage.ElapsedMonths(first, last)
			}
			if projElapsed <= 0 {
				projElapsed = elapsed
			}
			projActualPM = float64(projPeople) * projElapsed
		} else if proj.ActualPersonMonths > 0 {
			// Auto-detect: use git-derived actual person-months
			projActualPM = proj.ActualPersonMonths
			projPeople = proj.AuthorCount
			if projPeople == 0 {
				projPeople = 1
			}
			first, last, gitErr := leverage.GitDateRange(ctx, proj.GitDir)
			if gitErr == nil {
				projElapsed = leverage.ElapsedMonths(first, last)
			}
			if projElapsed <= 0 {
				projElapsed = elapsed
			}
		} else {
			// Fallback
			projPeople = proj.AuthorCount
			if projPeople == 0 {
				projPeople = 1
			}
			first, last, gitErr := leverage.GitDateRange(ctx, proj.GitDir)
			if gitErr == nil {
				projElapsed = leverage.ElapsedMonths(first, last)
			}
			if projElapsed <= 0 {
				projElapsed = elapsed
			}
			projActualPM = float64(projPeople) * projElapsed
		}

		score := leverage.ComputeScore(result, projPeople, projElapsed)
		score.ProjectName = proj.Name
		score.AuthorCount = proj.AuthorCount

		// Override with contribution-based actual person-months
		if projActualPM > 0 {
			score.ActualPersonMonths = projActualPM
			estPM := result.EstimatedPeople * result.EstimatedScheduleMonths
			score.FullLeverage = estPM / projActualPM
		}

		scores = append(scores, score)
	}

	// Check if we filtered to a non-existent project
	if projectFilter != "" && len(scores) == 0 {
		return fmt.Errorf("project not found: %s", projectFilter)
	}

	// Determine effective team size for aggregate calculations.
	// When cfg.ActualPeople == 0 (auto-detect), use max author count from scores.
	effectivePeople := cfg.ActualPeople
	if effectivePeople == 0 {
		for _, s := range scores {
			if s.AuthorCount > effectivePeople {
				effectivePeople = s.AuthorCount
			}
		}
		if effectivePeople == 0 {
			effectivePeople = 1
		}
	}

	// Aggregate campaign-wide totals
	agg := leverage.AggregateScores(scores, effectivePeople, elapsed)

	// Override with deduplicated campaign-wide actual person-months.
	// AggregateScores naively sums per-project ActualPersonMonths, which
	// double-counts authors who contribute across multiple repos.
	// CampaignActualPersonMonths merges authors by name across all git dirs.
	if authorFilter == "" && peopleOverride == 0 {
		campaignPM, pmErr := leverage.CampaignActualPersonMonths(ctx, resolved, setup.Resolver)
		if pmErr == nil && campaignPM > 0 {
			estPM := agg.EstimatedPeople * agg.EstimatedMonths
			agg.ActualPersonMonths = campaignPM
			agg.FullLeverage = estPM / campaignPM
		}
	}

	// Compute recent leverage from snapshots
	store := leverage.NewFileSnapshotStore(leverage.DefaultSnapshotDir(setup.Root))
	week7, has7 := leverage.RecentLeverage(ctx, store, scores, effectivePeople, now.AddDate(0, 0, -7))
	month30, has30 := leverage.RecentLeverage(ctx, store, scores, effectivePeople, now.AddDate(0, 0, -30))

	// Output based on format
	if jsonOut {
		return leverageOutputJSON(cmd, agg, scores)
	}

	if byAuthor {
		return leverageOutputByAuthor(cmd, agg, resolved, setup.Resolver)
	}

	recent := recentLeverage{week7: week7, has7: has7, month30: month30, has30: has30}
	opts := leverageOutputOpts{
		authorFilter:   authorFilter,
		authorExcluded: authorExcluded,
	}
	return leverageOutputTable(cmd, agg, scores, cfg, setup.AutoDetected, recent, opts)
}

