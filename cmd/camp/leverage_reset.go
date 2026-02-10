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
	Short: "Clear cached leverage snapshots to allow re-backfill",
	Long: `Reset deletes cached snapshot data so that backfill can regenerate it.

Without flags, all project snapshots are removed. Use --project to clear
only a single project's snapshots.

Examples:
  camp leverage reset                    Clear all snapshots
  camp leverage reset --project camp     Clear only camp's snapshots`,
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
	projectFilter, _ := cmd.Flags().GetString("project")

	targetDir := snapshotDir
	if projectFilter != "" {
		targetDir = filepath.Join(snapshotDir, projectFilter)
	}

	// Check if the target exists before removing.
	info, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(cmd.OutOrStdout(), "No snapshots to clear.")
			return nil
		}
		return fmt.Errorf("checking snapshot directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("expected directory at %s", targetDir)
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("removing snapshots: %w", err)
	}

	if projectFilter != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Cleared snapshots for project %q.\n", projectFilter)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Cleared all leverage snapshots.")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Run 'camp leverage backfill' to regenerate.")

	return nil
}
