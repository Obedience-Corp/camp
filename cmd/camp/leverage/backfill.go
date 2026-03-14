package leverage

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
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
	Cmd.AddCommand(leverageBackfillCmd)
}

func runLeverageBackfill(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()

	setup, err := initLeverageSetup(ctx)
	if err != nil {
		return err
	}
	cfg := setup.Cfg

	sinceStr, _ := cmd.Flags().GetString("since")
	if sinceStr != "" {
		since, err := time.Parse("2006-01-02", sinceStr)
		if err != nil {
			return camperrors.Wrapf(err, "invalid --since date %q (use YYYY-MM-DD)", sinceStr)
		}
		cfg.ProjectStart = since
	}

	runner, err := initRunner(cfg)
	if err != nil {
		return err
	}

	resolved, err := intleverage.ResolveProjects(ctx, setup.Root, cfg)
	if err != nil {
		return camperrors.Wrap(err, "resolving projects")
	}

	intleverage.PopulateProjectMetrics(ctx, resolved, setup.Resolver)

	projectFilter, _ := cmd.Flags().GetString("project")
	resolved, err = intleverage.FilterByName(resolved, projectFilter)
	if err != nil {
		return err
	}

	store := intleverage.NewFileSnapshotStore(intleverage.DefaultSnapshotDir(setup.Root))
	workers, _ := cmd.Flags().GetInt("workers")
	backfiller := intleverage.NewBackfiller(runner, store, workers)

	fmt.Fprintln(cmd.OutOrStdout(), "Backfilling leverage data...")
	backfiller.SetProgressCallback(func(project string, current, total int) {
		fmt.Fprintf(cmd.ErrOrStderr(), "  %s: %d/%d snapshots\n", project, current, total)
	})
	backfiller.SetWarningCallback(func(project, sample string, err error) {
		fmt.Fprintf(cmd.ErrOrStderr(), "  warning: %s @ %s: %v\n", project, sample, err)
	})

	start := time.Now()
	if err := backfiller.Run(ctx, resolved, cfg); err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "\nBackfill interrupted. Cleaning up...")
			return nil
		}
		return err
	}

	elapsed := time.Since(start)
	fmt.Fprintf(cmd.OutOrStdout(), "Done. Backfill completed in %s.\n", elapsed.Round(time.Second))
	return nil
}
