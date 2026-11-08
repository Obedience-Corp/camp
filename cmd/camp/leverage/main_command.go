package leverage

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

const LeverageJSONVersion = "leverage/v1alpha1"

// sccRunner is the package-level runner used by the leverage command.
// Tests can replace this to inject a mock.
var sccRunner intleverage.Runner
var leverageJSON bool

func init() {
	Cmd.Args = jsoncontract.Args(LeverageJSONVersion, func() bool { return leverageJSON }, cobra.MaximumNArgs(1))
	Cmd.RunE = func(cmd *cobra.Command, args []string) error {
		err := runLeverage(cmd, args)
		if done, ok := err.(*camperrors.CommandError); ok && done.Err == nil {
			return err
		}
		if err != nil && leverageJSON {
			return jsoncontract.RenderError(cmd, LeverageJSONVersion, err)
		}
		return err
	}
	Cmd.Flags().BoolVar(&leverageJSON, "json", false, "output as JSON")
	Cmd.Flags().StringP("project", "p", "", "filter by project name")
	Cmd.Flags().Int("people", 0, "override team size (0 = auto-detect from git)")
	Cmd.Flags().Bool("no-legend", false, "hide the leverage formula legend")
	Cmd.Flags().BoolP("verbose", "v", false, "show diagnostic details (config, project resolution, exclusions)")
	Cmd.Flags().String("author", "", "filter by author email (git substring match: 'alice@co' matches 'alice@co.com')")
	Cmd.Flags().Bool("by-author", false, "show a read-only per-author leverage breakdown")
	Cmd.Flags().Float64("author-threshold", 1, "minimum ownership percentage shown with --by-author (0 shows all)")
	Cmd.Flags().String("dir", "", "score a specific directory (skips camp project resolution)")
	addNoCommitFlag(Cmd)
	Cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(LeverageJSONVersion, func() bool { return leverageJSON }))
}

func runLeverage(cmd *cobra.Command, args []string) error {
	if err := validateReportFlags(cmd); err != nil {
		return err
	}
	targetDir, err := resolveTargetDir(cmd, args)
	if err != nil {
		return err
	}
	if targetDir != "" {
		projectFilter, _ := cmd.Flags().GetString("project")
		if projectFilter != "" {
			return camperrors.Newf("--project and --dir (or positional directory) are mutually exclusive")
		}
		return runLeverageDir(cmd, targetDir)
	}

	ctx := cmd.Context()
	root, detectErr := campaign.DetectCached(ctx)
	if detectErr != nil {
		if !camperrors.Is(detectErr, campaign.ErrNotInCampaign) {
			return detectErr
		}
		if project, _ := cmd.Flags().GetString("project"); project != "" {
			return camperrors.New("--project requires a camp; use a repository path instead")
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		return runLeverageDir(cmd, cwd)
	}
	if byAuthor, _ := cmd.Flags().GetBool("by-author"); byAuthor {
		return runRepositoryReport(cmd, "", root)
	}

	projectFilter, _ := cmd.Flags().GetString("project")
	peopleOverride, _ := cmd.Flags().GetInt("people")
	verbose, _ := cmd.Flags().GetBool("verbose")
	authorFilter, _ := cmd.Flags().GetString("author")
	byAuthor, _ := cmd.Flags().GetBool("by-author")

	// Explicit CLI conflict only. Config AuthorEmail is applied after setup and
	// is suppressed when --by-author is requested so the breakdown still works
	// in campaigns that set a personal default.
	if authorFilter != "" && byAuthor {
		return camperrors.New("--author and --by-author are mutually exclusive: use --by-author for the full per-author breakdown, or --author for personal leverage")
	}

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

	// Apply configured personal default only when not doing a by-author breakdown.
	if authorFilter == "" && !byAuthor && cfg.AuthorEmail != "" {
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

	authorMatch := intleverage.ExpandAuthorFilter(setup.Resolver, authorFilter)

	var scores []*intleverage.LeverageScore
	var snapshotInputs []intleverage.CurrentSnapshotInput
	// Keep projects aligned with scores for personal aggregate rewrite.
	var scoredProjects []intleverage.ResolvedProject
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

		score := intleverage.ComputeProjectScore(ctx, proj, result, intleverage.ProjectScoreParams{
			AuthorFilter:    authorFilter,
			AuthorMatch:     authorMatch,
			Resolver:        setup.Resolver,
			PeopleOverride:  peopleOverride,
			FallbackElapsed: elapsed,
		})
		scores = append(scores, score)
		scoredProjects = append(scoredProjects, proj)
		snapshotInputs = append(snapshotInputs, intleverage.CurrentSnapshotInput{
			Project: proj,
			Result:  result,
			Score:   score,
		})
	}

	if projectFilter != "" && len(scores) == 0 {
		return camperrors.Newf("project not found: %s", projectFilter)
	}

	effectivePeople := cfg.ActualPeople
	if authorFilter != "" {
		// Personal mode is always one effective person for simple leverage.
		effectivePeople = 1
	} else if effectivePeople == 0 {
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

	if authorFilter != "" {
		// Personal headline: union author calendar across unique git dirs
		// (never sum per-project spans) with already ownership-scaled estimates.
		authorPM, pmErr := intleverage.AuthorActualPersonMonths(ctx, scoredProjects, authorMatch, setup.Resolver)
		if pmErr != nil {
			return camperrors.Wrap(pmErr, "computing personal actual person-months")
		}
		if authorPM > 0 {
			estPM := agg.EstimatedPeople * agg.EstimatedMonths
			agg.ActualPersonMonths = authorPM
			agg.ActualPeople = 1
			agg.FullLeverage = estPM / authorPM
			if effectivePeople > 0 {
				agg.SimpleLeverage = agg.EstimatedPeople / float64(effectivePeople)
			}
		}
	} else if peopleOverride == 0 {
		campaignPM, pmErr := intleverage.CampaignActualPersonMonths(ctx, resolved, setup.Resolver)
		if pmErr == nil && campaignPM > 0 {
			estPM := agg.EstimatedPeople * agg.EstimatedMonths
			agg.ActualPersonMonths = campaignPM
			agg.FullLeverage = estPM / campaignPM
		}
	}

	store := intleverage.NewFileSnapshotStore(intleverage.DefaultSnapshotDir(setup.Root))
	if authorFilter == "" && peopleOverride == 0 {
		if err := intleverage.PersistCurrentSnapshots(ctx, store, snapshotInputs, now, nil); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to save leverage snapshots: %v\n", err)
		}
	}
	week7, has7 := intleverage.RecentLeverage(ctx, store, scores, effectivePeople, now.AddDate(0, 0, -7))
	month30, has30 := intleverage.RecentLeverage(ctx, store, scores, effectivePeople, now.AddDate(0, 0, -30))

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

	switch {
	case leverageJSON:
		err = leverageOutputJSON(cmd, agg, scores)
	case byAuthor:
		err = leverageOutputByAuthor(cmd, ctx, agg, scores, scoredProjects, setup.Resolver, opts)
	default:
		err = leverageOutputTable(cmd, agg, scores, cfg, setup.AutoDetected, recent, opts)
	}
	if err != nil {
		return err
	}

	autocommitLeverageData(ctx, cmd, autocommitWriter(cmd), autocommitInput{
		root:    setup.Root,
		cfg:     cfg,
		subject: "update scores",
		report:  renderScoreReport(agg, scores, cfg, opts),
	})
	return nil
}

func validateReportFlags(cmd *cobra.Command) error {
	byAuthor, _ := cmd.Flags().GetBool("by-author")
	author, _ := cmd.Flags().GetString("author")
	people, _ := cmd.Flags().GetInt("people")
	threshold, _ := cmd.Flags().GetFloat64("author-threshold")
	if byAuthor && author != "" {
		return camperrors.New("--author and --by-author are mutually exclusive")
	}
	if people < 0 {
		return camperrors.New("--people must be nonnegative")
	}
	if people > 0 && (byAuthor || author != "") {
		return camperrors.New("--people is only available for the repository or camp summary")
	}
	if cmd.Flags().Changed("author-threshold") && !byAuthor {
		return camperrors.New("--author-threshold requires --by-author")
	}
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 100 {
		return camperrors.New("--author-threshold must be a finite percentage from 0 to 100")
	}
	return nil
}
