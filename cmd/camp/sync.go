package main

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/peer"
	"github.com/Obedience-Corp/camp/internal/sync"
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
  1  Runtime failure (including pre-flight, sync, or update failure)
  2  Usage error (bad flags or args)
  3  Post-sync validation failed

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
  camp sync --json

  # Accelerate over a peer machine from ~/.obey/machines.yaml: for each
  # already-initialized submodule, fetch objects from that machine first
  # (LAN/tailnet), then run the normal origin-based update. Uninitialized
  # submodules skip the peer step and init from origin. Preflight, origin
  # URLs, validation, and exit codes are unchanged; an unreachable peer
  # degrades to a warning.
  camp sync --from studio-mac`,
	Args: cobra.ArbitraryArgs,
	RunE: jsoncontract.RunE(SyncJSONVersion, func() bool { return syncOpts.json }, runSync),
}

const SyncJSONVersion = "sync/v1alpha1"

var syncOpts struct {
	dryRun   bool
	force    bool
	verbose  bool
	parallel int
	noFetch  bool
	json     bool
	from     string
}

func init() {
	syncCmd.Flags().BoolVarP(&syncOpts.dryRun, "dry-run", "n", false,
		"Show what would happen without making changes")
	syncCmd.Flags().BoolVarP(&syncOpts.force, "force", "f", false,
		"Skip safety checks (uncommitted changes warning still shown)")
	syncCmd.Flags().BoolVarP(&syncOpts.verbose, "verbose", "v", false,
		"Show detailed output for each submodule")
	syncCmd.Flags().IntVarP(&syncOpts.parallel, "parallel", "p", 4,
		"Number of parallel git operations (git guards superproject ops with repo lockfiles that fail fast on contention; lower this if a slow disk surfaces transient lock errors)")
	syncCmd.Flags().BoolVar(&syncOpts.noFetch, "no-fetch", false,
		"Skip fetching from remote (use local refs only)")
	syncCmd.Flags().BoolVar(&syncOpts.json, "json", false,
		"Output results as JSON for scripting")
	syncCmd.Flags().StringVar(&syncOpts.from, "from", "",
		"Fetch objects for already-initialized submodules from this machine (id from ~/.obey/machines.yaml) before the origin update")

	rootCmd.AddCommand(syncCmd)
	syncCmd.GroupID = "campaign"
	syncCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(SyncJSONVersion, func() bool { return syncOpts.json }))
}

func runSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Detect campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Build syncer with options
	syncerOpts := []sync.SyncerOption{
		sync.WithDryRun(syncOpts.dryRun),
		sync.WithForce(syncOpts.force),
		sync.WithVerbose(syncOpts.verbose),
		sync.WithParallel(syncOpts.parallel),
		sync.WithNoFetch(syncOpts.noFetch),
		sync.WithJSON(syncOpts.json),
		sync.WithSubmodules(args),
	}
	if syncOpts.from != "" {
		src, err := resolveSyncPeer(ctx, campRoot, syncOpts.from)
		if err != nil {
			// Help text promises an unreachable peer degrades to a warning
			// and the git sync still runs via origin; a resolve failure
			// (unreachable/typo'd peer) must not abort the whole command.
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: warning: peer %q unavailable (%v); syncing via origin\n",
				syncOpts.from, err)
		} else {
			syncerOpts = append(syncerOpts, sync.WithPeer(src))
		}
	}
	syncer := sync.NewSyncer(campRoot, syncerOpts...)

	// Run preflight once for display, then pass it into Sync to avoid double execution
	preflight, err := syncer.RunPreflight(ctx)
	if err != nil {
		return camperrors.Wrap(err, "preflight checks")
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
			return camperrors.NewCommand("camp sync", sync.ExitPreflightFailed, "", nil)
		}
		return camperrors.NewCommand("camp sync", sync.ExitSyncFailed, "", nil)
	}

	return nil
}

// resolveSyncPeer maps --from to a peer source for this campaign: the local
// registry supplies the campaign's name, and the far machine's own camp
// resolves where that campaign lives there.
func resolveSyncPeer(ctx context.Context, campRoot, machineID string) (*peer.Source, error) {
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "load registry")
	}
	c, found := reg.FindByPath(campRoot)
	if !found {
		return nil, camperrors.Newf(
			"campaign at %s is not in the registry; --from needs the campaign's registered name to resolve it on %q",
			campRoot, machineID)
	}
	return peer.FromMachine(ctx, machineID, c.Name)
}
