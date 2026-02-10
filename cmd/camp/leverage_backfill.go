package main

import (
	"fmt"
	"os/signal"
	"time"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

var leverageBackfillCmd = &cobra.Command{
	Use:   "backfill",
	Short: "Reconstruct historical leverage data from git history",
	Long: `Backfill analyzes past commits to build leverage-over-time data.

Uses git worktrees to check out weekly snapshots, run scc analysis,
and compute leverage scores at each point in time. Results are stored
as snapshots for later retrieval via 'camp leverage history'.

Backfill is incremental: re-running only processes dates without
existing snapshots.

Examples:
  camp leverage backfill                       Backfill all projects
  camp leverage backfill --project camp        Backfill specific project
  camp leverage backfill --workers 2           Limit concurrency
  camp leverage backfill --since 2025-06-01    Backfill from June 2025`,
	RunE: runLeverageBackfill,
}

func init() {
	leverageBackfillCmd.Flags().StringP("project", "p", "", "backfill a single project")
	leverageBackfillCmd.Flags().IntP("workers", "w", 4, "number of parallel workers")
	leverageBackfillCmd.Flags().String("since", "", "start date (YYYY-MM-DD), overrides config project_start")
	leverageCmd.AddCommand(leverageBackfillCmd)
}

func runLeverageBackfill(cmd *cobra.Command, args []string) error {
	// Set up signal handling for clean Ctrl+C
	ctx, cancel := signal.NotifyContext(cmd.Context())
	defer cancel()

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	configPath := leverage.DefaultConfigPath(root)
	cfg, err := leverage.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.ProjectStart.IsZero() {
		detected, err := leverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return fmt.Errorf("auto-detecting config: %w", err)
		}
		cfg = detected
	}

	// Parse --since to override project_start for sampling
	sinceStr, _ := cmd.Flags().GetString("since")
	if sinceStr != "" {
		since, err := time.Parse("2006-01-02", sinceStr)
		if err != nil {
			return fmt.Errorf("invalid --since date %q (use YYYY-MM-DD): %w", sinceStr, err)
		}
		cfg.ProjectStart = since
	}

	// Create SCC runner
	runner := sccRunner
	if runner == nil {
		r, err := leverage.NewSCCRunner(cfg.COCOMOProjectType)
		if err != nil {
			return err
		}
		runner = r
	}

	resolved, err := leverage.ResolveProjects(ctx, root, cfg)
	if err != nil {
		return fmt.Errorf("resolving projects: %w", err)
	}

	// Apply --project filter
	projectFilter, _ := cmd.Flags().GetString("project")
	if projectFilter != "" {
		var filtered []leverage.ResolvedProject
		for _, p := range resolved {
			if p.Name == projectFilter {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("project not found: %s", projectFilter)
		}
		resolved = filtered
	}

	store := leverage.NewFileSnapshotStore(leverage.DefaultSnapshotDir(root))
	workers, _ := cmd.Flags().GetInt("workers")
	backfiller := leverage.NewBackfiller(runner, store, workers)

	// Set up progress output
	fmt.Fprintln(cmd.OutOrStdout(), "Backfilling leverage data...")
	backfiller.SetProgressCallback(func(project string, current, total int) {
		fmt.Fprintf(cmd.ErrOrStderr(), "  %s: %d/%d snapshots\n", project, current, total)
	})

	start := time.Now()
	if err := backfiller.Run(ctx, resolved, cfg); err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "\nBackfill interrupted. Cleaning up...")
			return nil // clean exit on Ctrl+C
		}
		return err
	}

	elapsed := time.Since(start)
	fmt.Fprintf(cmd.OutOrStdout(), "Done. Backfill completed in %s.\n", elapsed.Round(time.Second))
	return nil
}
