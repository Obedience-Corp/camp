package leverage

import (
	"fmt"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/spf13/cobra"
)

var leverageResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear all cached leverage data to allow full recomputation",
	Long: `Reset deletes cached snapshots and blame data so that leverage can
recompute from scratch.

Without flags, all project caches are removed. Use --project to clear
only a single project's data.

Examples:
  camp leverage reset                    Clear all cached data
  camp leverage reset --project camp     Clear only camp's cached data`,
	RunE: runLeverageReset,
}

func init() {
	leverageResetCmd.Flags().StringP("project", "p", "", "clear snapshots for a single project")
	Cmd.AddCommand(leverageResetCmd)
}

func runLeverageReset(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	setup, err := initLeverageSetup(ctx)
	if err != nil {
		return err
	}

	snapshotDir := intleverage.DefaultSnapshotDir(setup.Root)
	cacheDir := intleverage.DefaultCacheDir(setup.Root)
	projectFilter, _ := cmd.Flags().GetString("project")

	if projectFilter != "" {
		if err := project.ValidateProjectName(projectFilter); err != nil {
			return camperrors.Wrap(err, "--project flag")
		}
	}

	cleared := false

	if projectFilter != "" {
		snapshotTarget := filepath.Join(snapshotDir, projectFilter)
		if dirExists(snapshotDir) {
			if err := pathutil.ValidateBoundary(snapshotDir, snapshotTarget); err != nil {
				return camperrors.Wrapf(err, "snapshot path boundary violation for --project %q", projectFilter)
			}
		}
		if removeDirIfExists(snapshotTarget) {
			cleared = true
		}
	} else if removeDirIfExists(snapshotDir) {
		cleared = true
	}

	if projectFilter != "" {
		cacheFile := filepath.Join(cacheDir, projectFilter+".json")
		if dirExists(cacheDir) {
			if err := pathutil.ValidateBoundary(cacheDir, cacheFile); err != nil {
				return camperrors.Wrapf(err, "cache path boundary violation for --project %q", projectFilter)
			}
		}
		if err := os.Remove(cacheFile); err == nil {
			cleared = true
		}
	} else if removeDirIfExists(cacheDir) {
		cleared = true
	}

	if !cleared {
		fmt.Fprintln(cmd.OutOrStdout(), "No cached data to clear.")
		return nil
	}

	if projectFilter != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Cleared cached data for project %q.\n", projectFilter)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Cleared all cached leverage data.")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Run 'camp leverage backfill' to regenerate snapshots.")

	return nil
}

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func removeDirIfExists(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	return os.RemoveAll(dir) == nil
}
