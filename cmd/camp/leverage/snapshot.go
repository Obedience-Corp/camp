package leverage

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

var leverageSnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Capture current leverage state as a snapshot",
	Long: `Capture the current leverage state for all projects (or a specific project)
and save as JSON snapshots for historical tracking.

Each snapshot includes scc metrics, computed leverage scores, and per-author
LOC attribution from git blame.

Snapshots are stored in .campaign/leverage/snapshots/<project>/<date>.json.
Re-running on the same date overwrites the previous snapshot.

Examples:
  camp leverage snapshot                  Snapshot all projects
  camp leverage snapshot --project camp   Snapshot specific project`,
	RunE: runLeverageSnapshot,
}

func init() {
	leverageSnapshotCmd.Flags().StringP("project", "p", "", "snapshot a specific project only")
	Cmd.AddCommand(leverageSnapshotCmd)
}

func runLeverageSnapshot(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	setup, err := initLeverageSetup(ctx)
	if err != nil {
		return err
	}
	cfg := setup.Cfg

	runner, err := initRunner(cfg)
	if err != nil {
		return err
	}

	resolved, err := intleverage.ResolveProjects(ctx, setup.Root, cfg)
	if err != nil {
		return camperrors.Wrap(err, "resolving projects")
	}

	projectFilter, _ := cmd.Flags().GetString("project")
	store := intleverage.NewFileSnapshotStore(intleverage.DefaultSnapshotDir(setup.Root))
	elapsed := intleverage.ElapsedMonths(cfg.ProjectStart, time.Now())

	runPopulateMetrics(ctx, setup.Root, resolved, setup.Resolver, false)

	var count int
	for _, proj := range resolved {
		if err := ctx.Err(); err != nil {
			return err
		}
		if projectFilter != "" && proj.Name != projectFilter {
			continue
		}

		hash, commitDate, err := getHeadCommit(ctx, proj.GitDir)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s: %v\n", proj.Name, err)
			continue
		}

		result, err := runner.Run(ctx, proj.SCCDir, proj.ExcludeDirs)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s (scc): %v\n", proj.Name, err)
			continue
		}

		projPeople := cfg.ActualPeople
		if projPeople == 0 && proj.AuthorCount > 0 {
			projPeople = proj.AuthorCount
		}
		if projPeople == 0 {
			projPeople = 1
		}

		score := intleverage.ComputeScore(result, projPeople, elapsed)
		score.ProjectName = proj.Name
		score.AuthorCount = proj.AuthorCount

		if cfg.ActualPeople == 0 && proj.ActualPersonMonths > 0 {
			score.ActualPersonMonths = proj.ActualPersonMonths
			estPM := result.EstimatedPeople * result.EstimatedScheduleMonths
			score.FullLeverage = estPM / proj.ActualPersonMonths
		}

		scc := intleverage.SCCResultToSnapshotSCC(result)
		snapshot := intleverage.NewSnapshot(proj.Name, hash, commitDate, time.Now(), scc, score, proj.Authors)

		if err := store.Save(ctx, snapshot); err != nil {
			return camperrors.Wrapf(err, "saving snapshot for %s", proj.Name)
		}
		count++
	}

	if projectFilter != "" && count == 0 {
		return fmt.Errorf("project not found: %s", projectFilter)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Saved %d snapshots to .campaign/leverage/snapshots/\n", count)
	return nil
}

func getHeadCommit(ctx context.Context, gitDir string) (string, time.Time, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "-1", "--format=%H%n%cI")
	out, err := cmd.Output()
	if err != nil {
		return "", time.Time{}, camperrors.Wrap(err, "git log")
	}

	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) != 2 {
		return "", time.Time{}, fmt.Errorf("unexpected git log output")
	}

	date, err := time.Parse(time.RFC3339, lines[1])
	if err != nil {
		return "", time.Time{}, camperrors.Wrap(err, "parsing commit date")
	}

	return lines[0], date, nil
}
