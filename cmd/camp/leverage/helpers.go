package leverage

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

var populateMetrics func(ctx context.Context, campaignRoot string, resolved []intleverage.ResolvedProject, resolver *intleverage.AuthorResolver)

type leverageSetup struct {
	Root          string
	Cfg           *intleverage.LeverageConfig
	Resolver      *intleverage.AuthorResolver
	AutoDetected  bool
	ConfigCreated bool
}

func initLeverageSetup(ctx context.Context) (*leverageSetup, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "not in a campaign")
	}

	configPath := intleverage.DefaultConfigPath(root)
	_, statErr := os.Stat(configPath)
	configExists := statErr == nil

	cfg, err := intleverage.LoadConfig(configPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "loading config")
	}

	autoDetected := cfg.ProjectStart.IsZero()
	if autoDetected {
		detected, err := intleverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return nil, camperrors.Wrap(err, "auto-detecting config")
		}
		cfg = detected
	}

	if err := intleverage.PopulateProjects(ctx, root, cfg); err != nil {
		return nil, camperrors.Wrap(err, "populating projects")
	}
	if err := intleverage.SaveConfig(configPath, cfg); err != nil {
		return nil, camperrors.Wrap(err, "saving config")
	}

	configCreated := !configExists

	authorsPath := intleverage.DefaultAuthorsPath(root)
	authorCfg, err := intleverage.LoadAuthorConfig(authorsPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "loading authors config")
	}

	resolved, resolveErr := intleverage.ResolveProjects(ctx, root, cfg)
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
			detected, detectErr := intleverage.AutoDetectAuthors(ctx, gitDirs)
			if detectErr == nil && len(detected.Authors) > 0 {
				authorCfg = detected
				_ = intleverage.SaveAuthorConfig(authorsPath, authorCfg)
				fmt.Fprintf(os.Stderr, "Created authors config at .campaign/leverage/authors.json (%d authors)\n", len(authorCfg.Authors))
			}
		} else {
			detected, detectErr := intleverage.AutoDetectAuthors(ctx, gitDirs)
			if detectErr == nil && intleverage.SyncAuthors(authorCfg, detected) {
				_ = intleverage.SaveAuthorConfig(authorsPath, authorCfg)
			}
		}
	}

	resolver := intleverage.NewAuthorResolver(authorCfg)
	return &leverageSetup{
		Root:          root,
		Cfg:           cfg,
		Resolver:      resolver,
		AutoDetected:  autoDetected,
		ConfigCreated: configCreated,
	}, nil
}

func runPopulateMetrics(ctx context.Context, campaignRoot string, resolved []intleverage.ResolvedProject, resolver *intleverage.AuthorResolver, verbose bool) {
	if populateMetrics != nil {
		populateMetrics(ctx, campaignRoot, resolved, resolver)
		return
	}

	cache := intleverage.NewBlameCache(intleverage.DefaultCacheDir(campaignRoot))
	total := len(resolved)
	var cached, incremental, full int

	for i := range resolved {
		if err := ctx.Err(); err != nil {
			return
		}

		p := &resolved[i]
		hash, err := intleverage.ProjectHash(ctx, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Analyzing %s (%d/%d)...\n", p.Name, i+1, total)
			intleverage.PopulateOneProject(ctx, p, resolver)
			full++
			continue
		}

		entry, _ := cache.Load(ctx, p.Name)

		switch {
		case entry != nil && entry.CommitHash == hash && entry.SCCDir == p.SCCDir:
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s (%d/%d) cached\n", p.Name, i+1, total)
			}
			if count, err := intleverage.CountAuthors(ctx, p.GitDir, resolver); err == nil {
				p.AuthorCount = count
			} else {
				p.AuthorCount = entry.AuthorCount
			}
			p.ActualPersonMonths = entry.ActualPM
			p.Authors = entry.Authors
			cached++

		case entry != nil && entry.FileBlame != nil:
			subpath := ""
			if p.InMonorepo {
				rel, relErr := intleverage.RelPath(p.GitDir, p.SCCDir)
				if relErr == nil {
					subpath = rel
				}
			}

			modified, added, deleted, diffErr := intleverage.ChangedFiles(ctx, p.GitDir, entry.CommitHash, hash, subpath)
			if diffErr != nil {
				fmt.Fprintf(os.Stderr, "  Analyzing %s (%d/%d)...\n", p.Name, i+1, total)
				intleverage.PopulateOneProjectCached(ctx, p, cache, hash, resolver)
				full++
				continue
			}

			totalChanged := len(modified) + len(added) + len(deleted)
			fmt.Fprintf(os.Stderr, "  %s (%d/%d) updating %d files...\n", p.Name, i+1, total, totalChanged)

			if err := entry.IncrementalUpdate(ctx, p.SCCDir, modified, added, deleted); err != nil {
				intleverage.PopulateOneProjectCached(ctx, p, cache, hash, resolver)
				full++
				continue
			}

			entry.RecomputeAggregates()
			entry.CommitHash = hash
			entry.SCCDir = p.SCCDir
			intleverage.RecomputeProjectMetrics(ctx, p, entry, resolver)

			_ = cache.Save(ctx, entry)
			incremental++

		default:
			fmt.Fprintf(os.Stderr, "  Analyzing %s (%d/%d)...\n", p.Name, i+1, total)
			intleverage.PopulateOneProjectCached(ctx, p, cache, hash, resolver)
			full++
		}
	}

	if verbose || incremental > 0 || full > 0 {
		fmt.Fprintf(os.Stderr, "  Blame: %d cached, %d incremental, %d full\n", cached, incremental, full)
	}
}

func initRunner(cfg *intleverage.LeverageConfig) (intleverage.Runner, error) {
	if sccRunner != nil {
		return sccRunner, nil
	}
	return intleverage.NewSCCRunner(cfg.COCOMOProjectType)
}

func resolveAndPopulateProjects(ctx context.Context, root string, cfg *intleverage.LeverageConfig, resolver *intleverage.AuthorResolver, authorFilter string, verbose bool) ([]intleverage.ResolvedProject, int, error) {
	resolved, err := intleverage.ResolveProjects(ctx, root, cfg)
	if err != nil {
		return nil, 0, camperrors.Wrap(err, "resolving projects")
	}

	runPopulateMetrics(ctx, root, resolved, resolver, verbose)

	var authorExcluded int
	if authorFilter != "" {
		var filtered []intleverage.ResolvedProject
		for _, proj := range resolved {
			hasCommits, gitErr := intleverage.AuthorHasCommits(ctx, proj.GitDir, authorFilter)
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

func printVerboseLeverageInfo(cmd *cobra.Command, cfg *intleverage.LeverageConfig, setup *leverageSetup, resolved []intleverage.ResolvedProject) {
	configPath := intleverage.DefaultConfigPath(setup.Root)
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
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Projects in config: %d total, %d excluded\n", totalProjects, len(excludedNames))
	for _, n := range excludedNames {
		fmt.Fprintf(cmd.ErrOrStderr(), "[verbose]   excluded: %s\n", n)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Resolved projects: %d\n", len(resolved))
	for _, p := range resolved {
		fmt.Fprintf(cmd.ErrOrStderr(), "[verbose]   %s -> scc:%s git:%s\n", p.Name, p.SCCDir, p.GitDir)
	}
}

type scoreParams struct {
	AuthorFilter    string
	PeopleOverride  int
	FallbackElapsed float64
}

type currentSnapshotInput struct {
	project intleverage.ResolvedProject
	result  *intleverage.SCCResult
	score   *intleverage.LeverageScore
}

type headCommitResolver func(context.Context, string) (string, time.Time, error)

func computeProjectScore(ctx context.Context, proj intleverage.ResolvedProject, result *intleverage.SCCResult, params scoreParams) *intleverage.LeverageScore {
	var projActualPM float64
	var projPeople int
	var projElapsed float64

	if params.AuthorFilter != "" {
		projPeople = 1
		first, last, gitErr := intleverage.AuthorDateRange(ctx, proj.GitDir, params.AuthorFilter)
		if gitErr == nil {
			projElapsed = intleverage.ElapsedMonths(first, last)
		}
		if projElapsed <= 0 {
			projElapsed = 0.1
		}
		projActualPM = projElapsed
	} else if params.PeopleOverride > 0 {
		projPeople = params.PeopleOverride
		first, last, gitErr := intleverage.GitDateRange(ctx, proj.GitDir)
		if gitErr == nil {
			projElapsed = intleverage.ElapsedMonths(first, last)
		}
		if projElapsed <= 0 {
			projElapsed = params.FallbackElapsed
		}
		projActualPM = float64(projPeople) * projElapsed
	} else if proj.ActualPersonMonths > 0 {
		projActualPM = proj.ActualPersonMonths
		projPeople = proj.AuthorCount
		if projPeople == 0 {
			projPeople = 1
		}
		first, last, gitErr := intleverage.GitDateRange(ctx, proj.GitDir)
		if gitErr == nil {
			projElapsed = intleverage.ElapsedMonths(first, last)
		}
		if projElapsed <= 0 {
			projElapsed = params.FallbackElapsed
		}
	} else {
		projPeople = proj.AuthorCount
		if projPeople == 0 {
			projPeople = 1
		}
		first, last, gitErr := intleverage.GitDateRange(ctx, proj.GitDir)
		if gitErr == nil {
			projElapsed = intleverage.ElapsedMonths(first, last)
		}
		if projElapsed <= 0 {
			projElapsed = params.FallbackElapsed
		}
		projActualPM = float64(projPeople) * projElapsed
	}

	score := intleverage.ComputeScore(result, projPeople, projElapsed)
	score.ProjectName = proj.Name
	score.AuthorCount = proj.AuthorCount

	if projActualPM > 0 {
		score.ActualPersonMonths = projActualPM
		estPM := result.EstimatedPeople * result.EstimatedScheduleMonths
		score.FullLeverage = estPM / projActualPM
	}

	return score
}

func persistCurrentSnapshots(ctx context.Context, store intleverage.SnapshotStorer, inputs []currentSnapshotInput, sampledAt time.Time, resolveHead headCommitResolver) error {
	if resolveHead == nil {
		resolveHead = getHeadCommit
	}

	// TODO: Add snapshot retention trimming. Daily automatic snapshots solve
	// recent-history freshness but will grow unbounded without an age-based cap.
	type commitMeta struct {
		hash string
		date time.Time
	}

	byGitDir := make(map[string]commitMeta)

	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return err
		}

		meta, ok := byGitDir[input.project.GitDir]
		if !ok {
			hash, date, err := resolveHead(ctx, input.project.GitDir)
			if err != nil {
				return camperrors.Wrapf(err, "reading HEAD commit for %s", input.project.Name)
			}
			meta = commitMeta{hash: hash, date: date}
			byGitDir[input.project.GitDir] = meta
		}

		scc := intleverage.SCCResultToSnapshotSCC(input.result)
		snapshot := intleverage.NewSnapshot(input.project.Name, meta.hash, meta.date, sampledAt, scc, input.score, input.project.Authors)
		if err := store.Save(ctx, snapshot); err != nil {
			return camperrors.Wrapf(err, "saving current snapshot for %s", input.project.Name)
		}
	}

	return nil
}

func buildScoreRows(scores []*intleverage.LeverageScore) [][]string {
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
