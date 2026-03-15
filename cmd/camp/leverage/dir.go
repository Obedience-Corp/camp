package leverage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

func resolveTargetDir(cmd *cobra.Command, args []string) (string, error) {
	dirFlag, _ := cmd.Flags().GetString("dir")

	target := dirFlag
	if target == "" && len(args) > 0 {
		target = args[0]
	}
	if target == "" {
		return "", nil
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return "", camperrors.Wrap(err, "resolving directory path")
	}
	return abs, nil
}

func initDirectorySetup(ctx context.Context, targetDir, gitRoot string) (*leverageSetup, error) {
	info, err := os.Stat(targetDir)
	if err != nil {
		return nil, camperrors.Wrap(err, "directory not found")
	}
	if !info.IsDir() {
		return nil, camperrors.Wrapf(fmt.Errorf("path is not a directory"), "%s", targetDir)
	}

	root, campaignErr := campaign.Detect(ctx, targetDir)
	hasCampaign := campaignErr == nil && root != ""

	var cfg *intleverage.LeverageConfig
	var resolver *intleverage.AuthorResolver
	autoDetected := true

	if hasCampaign {
		configPath := intleverage.DefaultConfigPath(root)
		loaded, loadErr := intleverage.LoadConfig(configPath)
		if loadErr == nil && !loaded.ProjectStart.IsZero() {
			cfg = loaded
			autoDetected = false
		}

		authorsPath := intleverage.DefaultAuthorsPath(root)
		authorCfg, authErr := intleverage.LoadAuthorConfig(authorsPath)
		if authErr == nil && authorCfg != nil {
			resolver = intleverage.NewAuthorResolver(authorCfg)
		}
	}

	if cfg == nil {
		cfg = &intleverage.LeverageConfig{
			COCOMOProjectType: "organic",
		}

		if gitRoot != "" {
			first, _, gitErr := intleverage.GitDateRange(ctx, gitRoot)
			if gitErr == nil {
				cfg.ProjectStart = first
			}
		}

		if cfg.ProjectStart.IsZero() {
			cfg.ProjectStart = time.Now().AddDate(0, -1, 0)
		}
	}

	if resolver == nil {
		resolver = intleverage.NewAuthorResolver(nil)
	}

	return &leverageSetup{
		Root:         root,
		Cfg:          cfg,
		Resolver:     resolver,
		AutoDetected: autoDetected,
	}, nil
}

func detectGitRoot(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func resolveDirectoryProject(targetDir, gitRoot string) intleverage.ResolvedProject {
	proj := intleverage.ResolvedProject{
		Name:   filepath.Base(targetDir),
		SCCDir: targetDir,
	}

	if gitRoot != "" {
		proj.GitDir = gitRoot
		proj.InMonorepo = targetDir != gitRoot
	} else {
		proj.GitDir = targetDir
	}

	return proj
}

func runLeverageDir(cmd *cobra.Command, targetDir string) error {
	ctx := cmd.Context()

	jsonOut, _ := cmd.Flags().GetBool("json")
	peopleOverride, _ := cmd.Flags().GetInt("people")
	verbose, _ := cmd.Flags().GetBool("verbose")
	authorFilter, _ := cmd.Flags().GetString("author")
	byAuthor, _ := cmd.Flags().GetBool("by-author")

	gitRoot := detectGitRoot(ctx, targetDir)

	setup, err := initDirectorySetup(ctx, targetDir, gitRoot)
	if err != nil {
		return err
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

	proj := resolveDirectoryProject(targetDir, gitRoot)
	resolved := []intleverage.ResolvedProject{proj}
	if setup.Root != "" {
		runPopulateMetrics(ctx, setup.Root, resolved, setup.Resolver, verbose)
	} else {
		intleverage.PopulateOneProject(ctx, &resolved[0], setup.Resolver)
	}
	proj = resolved[0]

	if authorFilter != "" {
		hasCommits, gitErr := intleverage.AuthorHasCommits(ctx, proj.GitDir, authorFilter)
		if gitErr != nil || !hasCommits {
			return camperrors.Wrapf(fmt.Errorf("no commits for author"), "%s in %s", authorFilter, proj.Name)
		}
	}

	if verbose {
		fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Directory mode: %s\n", targetDir)
		if setup.Root != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Campaign detected: %s\n", setup.Root)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] No campaign detected\n")
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] Git root: %s\n", proj.GitDir)
		fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] InMonorepo: %v\n", proj.InMonorepo)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	now := time.Now()
	elapsed := intleverage.ElapsedMonths(cfg.ProjectStart, now)

	result, err := runner.Run(ctx, proj.SCCDir, proj.ExcludeDirs)
	if err != nil {
		return camperrors.Wrapf(err, "running scc on %s", proj.Name)
	}

	score := computeProjectScore(ctx, proj, result, scoreParams{
		AuthorFilter:    authorFilter,
		PeopleOverride:  peopleOverride,
		FallbackElapsed: elapsed,
	})
	scores := []*intleverage.LeverageScore{score}

	effectivePeople := cfg.ActualPeople
	if effectivePeople == 0 {
		effectivePeople = proj.AuthorCount
		if effectivePeople == 0 {
			effectivePeople = 1
		}
	}
	agg := intleverage.AggregateScores(scores, effectivePeople, elapsed)

	if score.ActualPersonMonths > 0 {
		estPM := agg.EstimatedPeople * agg.EstimatedMonths
		agg.ActualPersonMonths = score.ActualPersonMonths
		agg.FullLeverage = estPM / score.ActualPersonMonths
	}

	opts := leverageOutputOpts{
		authorFilter:  authorFilter,
		directoryMode: true,
		directoryName: proj.Name,
	}

	if jsonOut {
		return leverageOutputJSON(cmd, agg, scores)
	}
	if byAuthor {
		return leverageOutputByAuthor(cmd, agg, resolved, setup.Resolver, opts)
	}

	return leverageOutputTable(cmd, agg, scores, cfg, setup.AutoDetected, recentLeverage{}, opts)
}
