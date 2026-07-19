package intent

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	wkcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	intentsync "github.com/Obedience-Corp/camp/internal/intent/sync"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Reconcile intents against their tracked GitHub PRs",
	Long: `For every non-dungeon intent whose work_ref contains a GitHub PR URL,
query the PR's state via the gh CLI. Intents whose PR has merged are moved to
dungeon/done automatically, with a decision record and a ledger event. Intents
whose PR closed without merging are reported but never auto-moved -- resolve
those manually with 'camp intent move' or 'camp intent release'.

Requires the gh CLI (https://cli.github.com) on PATH with an authenticated
'gh auth login' session. Intents with no PR reference in work_ref are skipped
without needing gh at all.

Examples:
  camp intent sync              Reconcile and auto-close merged PRs
  camp intent sync --dry-run    Preview without moving anything`,
	Args: cobra.NoArgs,
	RunE: runIntentSync,
}

func init() {
	Cmd.AddCommand(intentSyncCmd)

	flags := intentSyncCmd.Flags()
	flags.Bool("dry-run", false, "Preview without moving anything")
	flags.Bool("no-commit", false, "Don't create a git commit")
}

func runIntentSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	svc.SetLedger(ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())))
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring intent directories")
	}

	candidates, err := syncCandidates(ctx, svc)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		fmt.Println("No intents with a tracked GitHub PR reference.")
		return nil
	}

	checker, err := intentsync.NewGHChecker()
	if err != nil {
		return camperrors.Wrap(err, "camp intent sync requires the gh CLI")
	}

	decisions, err := intentsync.Plan(ctx, checker, candidates)
	if err != nil {
		return camperrors.Wrap(err, "resolving PR state")
	}

	return reportSyncDecisions(cmd, ctx, svc, resolver.Intents(), cfg, campaignRoot, dryRun, noCommit, decisions)
}

// syncCandidates lists every non-dungeon intent whose work_ref resolves to a
// GitHub PR URL. gh is never invoked when this returns empty.
func syncCandidates(ctx context.Context, svc *intent.IntentService) ([]*intent.Intent, error) {
	all, err := svc.List(ctx, nil)
	if err != nil {
		return nil, camperrors.Wrap(err, "listing intents")
	}

	candidates := make([]*intent.Intent, 0, len(all))
	for _, i := range all {
		if i.Status.InDungeon() {
			continue
		}
		if intentsync.PRURLFromRefs(i.WorkRef) != "" {
			candidates = append(candidates, i)
		}
	}
	return candidates, nil
}

// reportSyncDecisions prints one line per decision, applies merged decisions
// (unless dryRun), and prints a final tally. A move failure on one intent is
// reported and counted but does not stop the rest of the batch from being
// reconciled -- a reconciliation command should make as much forward progress
// as it safely can in one run. The returned error is non-nil (causing a
// non-zero exit code) only when at least one merged intent failed to move.
func reportSyncDecisions(cmd *cobra.Command, ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, dryRun, noCommit bool, decisions []intentsync.Decision) error {
	var moved, closedReported, checkFailed, moveFailed int
	for _, d := range decisions {
		switch d.Outcome {
		case intentsync.OutcomeMerged:
			if dryRun {
				moved++
				fmt.Printf("Would move to dungeon/done: %s (%s) -- PR merged: %s\n", d.Title, d.IntentID, d.PRURL)
				continue
			}
			if err := applyMergedDecision(ctx, svc, intentsDir, cfg, campaignRoot, noCommit, d); err != nil {
				moveFailed++
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to move %s (%s) to dungeon/done: %v\n", d.Title, d.IntentID, err)
				continue
			}
			moved++
			fmt.Printf("✓ Moved to dungeon/done: %s (%s) -- PR merged: %s\n", d.Title, d.IntentID, d.PRURL)
		case intentsync.OutcomeClosed:
			closedReported++
			fmt.Printf("! PR closed without merging, not auto-moved: %s (%s) -- %s\n", d.Title, d.IntentID, d.PRURL)
		case intentsync.OutcomeCheckFailed:
			checkFailed++
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not resolve PR state for %s (%s): %v\n", d.Title, d.IntentID, d.Err)
		case intentsync.OutcomeOpen:
			// Still open; nothing to report.
		}
	}

	fmt.Printf("\n%d checked, %d merged, %d closed unmerged, %d check failed\n", len(decisions), moved, closedReported, checkFailed)
	if moveFailed > 0 {
		return camperrors.Newf("%d merged intent(s) failed to move to dungeon/done", moveFailed)
	}
	return nil
}

// applyMergedDecision performs the filesystem move for one merged decision
// and records the audit + commit trail, mirroring camp intent move/archive.
func applyMergedDecision(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, d intentsync.Decision) error {
	result, err := intentsync.Apply(ctx, svc, d)
	if err != nil {
		return err
	}

	if err := appendIntentAuditEvent(ctx, intentsDir, audit.Event{
		Type:   audit.EventSync,
		ID:     result.ID,
		Title:  result.Title,
		To:     string(result.Status),
		Reason: fmt.Sprintf("PR merged: %s", d.PRURL),
	}); err != nil {
		return err
	}

	if !noCommit {
		opts := wkcmd.AmbientCommitOptions(ctx, campaignRoot, cfg.ID, os.Stderr)
		opts.Files = commit.NormalizeFiles(campaignRoot, result.Path, audit.FilePath(intentsDir))
		opts.SelectiveOnly = true
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options:     opts,
			Action:      commit.IntentSync,
			IntentTitle: result.Title,
			Description: fmt.Sprintf("PR merged: %s", d.PRURL),
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
		commit.WarnIfSkipped(os.Stderr, commitResult)
	}

	return nil
}
