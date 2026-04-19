package leverage

import (
	"fmt"
	"time"

	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

// sccRunner is the package-level runner used by the leverage command.
// Tests can replace this to inject a mock.
var sccRunner intleverage.Runner

func init() {
	Cmd.RunE = runLeverage
	Cmd.Flags().Bool("json", false, "output as JSON")
	Cmd.Flags().StringP("project", "p", "", "filter by project name")
	Cmd.Flags().Int("people", 0, "override team size (0 = auto-detect from git)")
	Cmd.Flags().Bool("no-legend", false, "hide the leverage formula legend")
	Cmd.Flags().BoolP("verbose", "v", false, "show diagnostic details (config, project resolution, exclusions)")
	Cmd.Flags().String("author", "", "filter by author email (git substring match — 'alice@co' matches 'alice@co.com')")
	Cmd.Flags().Bool("by-author", false, "show per-author leverage breakdown")
	Cmd.Flags().String("dir", "", "score a specific directory (skips campaign project resolution)")
}

func runLeverage(cmd *cobra.Command, args []string) error {
	targetDir, err := resolveTargetDir(cmd, args)
	if err != nil {
		return err
	}
	if targetDir != "" {
		projectFilter, _ := cmd.Flags().GetString("project")
		if projectFilter != "" {
			return fmt.Errorf("--project and --dir (or positional directory) are mutually exclusive")
		}
		return runLeverageDir(cmd, targetDir)
	}

	ctx := cmd.Context()

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

	if peopleOverride > 0 {
		cfg.ActualPeople = peopleOverride
	}

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

	if verbose {
		printVerboseLeverageInfo(cmd, cfg, setup, resolved)
	}

	now := time.Now()
	elapsed := intleverage.ElapsedMonths(cfg.ProjectStart, now)

	var scores []*intleverage.LeverageScore
	var snapshotInputs []currentSnapshotInput
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

		score := computeProjectScore(ctx, proj, result, scoreParams{
			AuthorFilter:    authorFilter,
			PeopleOverride:  peopleOverride,
			FallbackElapsed: elapsed,
		})
		scores = append(scores, score)
		snapshotInputs = append(snapshotInputs, currentSnapshotInput{
			project: proj,
			result:  result,
			score:   score,
		})
	}

	if projectFilter != "" && len(scores) == 0 {
		return fmt.Errorf("project not found: %s", projectFilter)
	}

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

	agg := intleverage.AggregateScores(scores, effectivePeople, elapsed)

	if authorFilter == "" && peopleOverride == 0 {
		campaignPM, pmErr := intleverage.CampaignActualPersonMonths(ctx, resolved, setup.Resolver)
		if pmErr == nil && campaignPM > 0 {
			estPM := agg.EstimatedPeople * agg.EstimatedMonths
			agg.ActualPersonMonths = campaignPM
			agg.FullLeverage = estPM / campaignPM
		}
	}

	store := intleverage.NewFileSnapshotStore(intleverage.DefaultSnapshotDir(setup.Root))
	if authorFilter == "" && peopleOverride == 0 {
		if err := persistCurrentSnapshots(ctx, store, snapshotInputs, now, nil); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to save leverage snapshots: %v\n", err)
		}
	}
	week7, has7 := intleverage.RecentLeverage(ctx, store, scores, effectivePeople, now.AddDate(0, 0, -7))
	month30, has30 := intleverage.RecentLeverage(ctx, store, scores, effectivePeople, now.AddDate(0, 0, -30))

	if jsonOut {
		return leverageOutputJSON(cmd, agg, scores)
	}

	recent := recentLeverage{
		week7:         week7,
		has7:          has7,
		month30:       month30,
		has30:         has30,
		needsBackfill: authorFilter == "" && peopleOverride == 0 && len(scores) > 0 && !has7 && !has30,
	}
	opts := leverageOutputOpts{
		authorFilter:   authorFilter,
		authorExcluded: authorExcluded,
	}

	if byAuthor {
		return leverageOutputByAuthor(cmd, agg, resolved, setup.Resolver, opts)
	}

	return leverageOutputTable(cmd, agg, scores, cfg, setup.AutoDetected, recent, opts)
}
