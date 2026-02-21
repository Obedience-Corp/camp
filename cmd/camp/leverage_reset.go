package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/obediencecorp/camp/internal/leverage"
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
	leverageCmd.AddCommand(leverageResetCmd)
}

func runLeverageReset(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	setup, err := initLeverageSetup(ctx)
	if err != nil {
		return err
	}

	snapshotDir := leverage.DefaultSnapshotDir(setup.Root)
	cacheDir := leverage.DefaultCacheDir(setup.Root)
	projectFilter, _ := cmd.Flags().GetString("project")

	cleared := false

	// Clear snapshots.
	if projectFilter != "" {
		if removeDirIfExists(filepath.Join(snapshotDir, projectFilter)) {
			cleared = true
		}
	} else {
		if removeDirIfExists(snapshotDir) {
			cleared = true
		}
	}

	// Clear blame cache.
	if projectFilter != "" {
		cacheFile := filepath.Join(cacheDir, projectFilter+".json")
		if err := os.Remove(cacheFile); err == nil {
			cleared = true
		}
	} else {
		if removeDirIfExists(cacheDir) {
			cleared = true
		}
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

// removeDirIfExists removes a directory if it exists. Returns true if something
// was removed.
func removeDirIfExists(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	return os.RemoveAll(dir) == nil
}
