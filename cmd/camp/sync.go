package main

import (
	"context"
	"fmt"
	"os"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/sync"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync [submodule...]",
	Short: "Safely synchronize submodules",
	Long: `Synchronize submodules with pre-flight safety checks.

The sync command performs three critical operations:

  1. PRE-FLIGHT CHECKS
     Verifies no uncommitted changes or unpushed commits that could
     be lost during synchronization.

  2. URL SYNCHRONIZATION
     Copies URLs from .gitmodules to .git/config, fixing URL mismatches
     that occur when remote URLs change.

  3. SUBMODULE UPDATE
     Fetches and checks out the correct commits for all submodules.

This order is critical: sync-before-update prevents silent code deletion
when URLs change on remote repositories.

EXIT CODES:
  0  Success
  1  Pre-flight check failed (uncommitted changes)
  2  Sync or update operation failed
  3  Post-sync validation failed
  4  Invalid arguments

EXAMPLES:
  # Sync all submodules (recommended default)
  camp sync

  # Preview what would happen without making changes
  camp sync --dry-run

  # Sync a specific submodule only
  camp sync projects/camp

  # Force sync despite uncommitted changes (dangerous!)
  camp sync --force

  # Detailed output for each submodule
  camp sync --verbose

  # JSON output for scripting
  camp sync --json`,
	Args: cobra.ArbitraryArgs,
	RunE: runSync,
}

var syncOpts struct {
	dryRun   bool
	force    bool
	verbose  bool
	parallel int
	noFetch  bool
	json     bool
}

func init() {
	syncCmd.Flags().BoolVarP(&syncOpts.dryRun, "dry-run", "n", false,
		"Show what would happen without making changes")
	syncCmd.Flags().BoolVarP(&syncOpts.force, "force", "f", false,
		"Skip safety checks (uncommitted changes warning still shown)")
	syncCmd.Flags().BoolVarP(&syncOpts.verbose, "verbose", "v", false,
		"Show detailed output for each submodule")
	syncCmd.Flags().IntVarP(&syncOpts.parallel, "parallel", "p", 4,
		"Number of parallel git operations")
	syncCmd.Flags().BoolVar(&syncOpts.noFetch, "no-fetch", false,
		"Skip fetching from remote (use local refs only)")
	syncCmd.Flags().BoolVar(&syncOpts.json, "json", false,
		"Output results as JSON for scripting")

	rootCmd.AddCommand(syncCmd)
	syncCmd.GroupID = "campaign"
}

func runSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Detect campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	// Build syncer with options
	syncer := sync.NewSyncer(campRoot,
		sync.WithDryRun(syncOpts.dryRun),
		sync.WithForce(syncOpts.force),
		sync.WithVerbose(syncOpts.verbose),
		sync.WithParallel(syncOpts.parallel),
		sync.WithNoFetch(syncOpts.noFetch),
		sync.WithJSON(syncOpts.json),
		sync.WithSubmodules(args),
	)

	// Run preflight once for display, then pass it into Sync to avoid double execution
	preflight, err := syncer.RunPreflight(ctx)
	if err != nil {
		return fmt.Errorf("preflight checks: %w", err)
	}

	// Inject the preflight result so Sync() won't re-run it
	syncer.SetPreflightResult(preflight)

	// Run sync
	result, err := syncer.Sync(ctx)
	if err != nil {
		return err
	}

	// Build options for formatter
	opts := syncOptions{
		dryRun:   syncOpts.dryRun,
		force:    syncOpts.force,
		verbose:  syncOpts.verbose,
		parallel: syncOpts.parallel,
		noFetch:  syncOpts.noFetch,
		json:     syncOpts.json,
	}

	// Format and display output
	formatSyncResult(result, opts, preflight)

	// Return appropriate exit code
	if !result.Success {
		if !result.PreflightPassed && !syncOpts.force {
			os.Exit(sync.ExitPreflightFailed)
		}
		os.Exit(sync.ExitSyncFailed)
	}

	return nil
}
