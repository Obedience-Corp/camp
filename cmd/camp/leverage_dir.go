package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/leverage"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

// resolveTargetDir returns the target directory from --dir flag or positional arg.
// Returns empty string if neither is provided (meaning: use normal campaign mode).
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

// initDirectorySetup creates a leverageSetup for directory mode.
// It opportunistically loads campaign config if available, otherwise uses sensible defaults.
// gitRoot is the pre-computed git toplevel (empty if not a git repo).
func initDirectorySetup(ctx context.Context, targetDir, gitRoot string) (*leverageSetup, error) {
	// Validate directory exists
	info, err := os.Stat(targetDir)
	if err != nil {
		return nil, camperrors.Wrap(err, "directory not found")
	}
	if !info.IsDir() {
		return nil, camperrors.Wrapf(fmt.Errorf("path is not a directory"), "%s", targetDir)
	}

	// Try to detect campaign opportunistically
	root, campaignErr := campaign.DetectCached(ctx)
	hasCampaign := campaignErr == nil && root != ""

	var cfg *leverage.LeverageConfig
	var resolver *leverage.AuthorResolver
	autoDetected := true

	if hasCampaign {
		// Load campaign config for better defaults
		configPath := leverage.DefaultConfigPath(root)
		loaded, loadErr := leverage.LoadConfig(configPath)
		if loadErr == nil && !loaded.ProjectStart.IsZero() {
			cfg = loaded
			autoDetected = false
		}

		// Load author config if available
		authorsPath := leverage.DefaultAuthorsPath(root)
		authorCfg, authErr := leverage.LoadAuthorConfig(authorsPath)
		if authErr == nil && authorCfg != nil {
			resolver = leverage.NewAuthorResolver(authorCfg)
		}
	}

	if cfg == nil {
		// No campaign or no saved config: use defaults with git-detected start date
		cfg = &leverage.LeverageConfig{
			COCOMOProjectType: "organic",
		}

		if gitRoot != "" {
			first, _, gitErr := leverage.GitDateRange(ctx, gitRoot)
			if gitErr == nil {
				cfg.ProjectStart = first
			}
		}

		if cfg.ProjectStart.IsZero() {
			cfg.ProjectStart = time.Now().AddDate(0, -1, 0) // fallback: 1 month ago
		}
	}

	if resolver == nil {
		resolver = leverage.NewAuthorResolver(nil)
	}

	return &leverageSetup{
		Root:         root, // empty if no campaign
		Cfg:          cfg,
		Resolver:     resolver,
		AutoDetected: autoDetected,
	}, nil
}

// detectGitRoot returns the git toplevel for a directory, or empty string if not a git repo.
func detectGitRoot(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resolveDirectoryProject builds a single ResolvedProject for the target directory.
// gitRoot is the pre-computed git toplevel (empty if not a git repo).
func resolveDirectoryProject(targetDir, gitRoot string) leverage.ResolvedProject {
	proj := leverage.ResolvedProject{
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

// runLeverageDir is the main entry point for directory mode.
func runLeverageDir(cmd *cobra.Command, targetDir string) error {
	ctx := cmd.Context()

	// Parse flags
	jsonOut, _ := cmd.Flags().GetBool("json")
	peopleOverride, _ := cmd.Flags().GetInt("people")
	verbose, _ := cmd.Flags().GetBool("verbose")
	authorFilter, _ := cmd.Flags().GetString("author")
	byAuthor, _ := cmd.Flags().GetBool("by-author")

	// Detect git root once, pass to both setup and project resolution
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

	// Build single project
	proj := resolveDirectoryProject(targetDir, gitRoot)

	// Populate metrics
	resolved := []leverage.ResolvedProject{proj}
	if setup.Root != "" {
		// Inside a campaign: use blame cache
		runPopulateMetrics(ctx, setup.Root, resolved, setup.Resolver, verbose)
	} else {
		// No campaign: direct compute
		leverage.PopulateOneProject(ctx, &resolved[0], setup.Resolver)
	}
	proj = resolved[0]

	// Filter by author if needed
	if authorFilter != "" {
		hasCommits, gitErr := leverage.AuthorHasCommits(ctx, proj.GitDir, authorFilter)
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

	// Check context before expensive scc run
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Compute elapsed
	now := time.Now()
	elapsed := leverage.ElapsedMonths(cfg.ProjectStart, now)

	// Run scc
	result, err := runner.Run(ctx, proj.SCCDir, proj.ExcludeDirs)
	if err != nil {
		return camperrors.Wrapf(err, "running scc on %s", proj.Name)
	}

	// Compute score using shared logic
	score := computeProjectScore(ctx, proj, result, scoreParams{
		AuthorFilter:    authorFilter,
		PeopleOverride:  peopleOverride,
		FallbackElapsed: elapsed,
	})
	scores := []*leverage.LeverageScore{score}

	// Aggregate (single project, so aggregate == project score)
	effectivePeople := cfg.ActualPeople
	if effectivePeople == 0 {
		effectivePeople = proj.AuthorCount
		if effectivePeople == 0 {
			effectivePeople = 1
		}
	}
	agg := leverage.AggregateScores(scores, effectivePeople, elapsed)

	// Override aggregate with project-level actual PM (single project, no dedup needed)
	if score.ActualPersonMonths > 0 {
		estPM := agg.EstimatedPeople * agg.EstimatedMonths
		agg.ActualPersonMonths = score.ActualPersonMonths
		agg.FullLeverage = estPM / score.ActualPersonMonths
	}

	// Output
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

	// No snapshots in directory mode
	return leverageOutputTable(cmd, agg, scores, cfg, setup.AutoDetected, recentLeverage{}, opts)
}
