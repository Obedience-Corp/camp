package intent

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	wkcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentReleaseCmd = &cobra.Command{
	Use:   "release <id>",
	Short: "Release an intent's assignment",
	Long: `Clear an intent's assigned_to and assigned_at, returning it to the
unclaimed pool. Any recorded work_ref entries (PR URLs, branches, festival
paths) are left in place so a later camp intent sync can still resolve them.

Examples:
  camp intent release add-dark`,
	Args: cobra.ExactArgs(1),
	RunE: runIntentRelease,
}

func init() {
	Cmd.AddCommand(intentReleaseCmd)

	intentReleaseCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
}

func runIntentRelease(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
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

	before, err := svc.Find(ctx, id)
	if err != nil {
		return camperrors.Newf("intent not found: %s", id)
	}
	if before.AssignedTo == "" {
		fmt.Printf("Intent already unassigned: %s\n", before.Path)
		return nil
	}
	previousAssignee := before.AssignedTo

	result, err := svc.Release(ctx, id)
	if err != nil {
		return camperrors.Wrap(err, "failed to release intent")
	}

	fmt.Printf("✓ Intent released (was claimed by %s): %s\n", previousAssignee, result.Path)

	if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
		Type:   audit.EventRelease,
		ID:     result.ID,
		Title:  result.Title,
		To:     string(result.Status),
		Reason: fmt.Sprintf("released from %s", previousAssignee),
	}); err != nil {
		return err
	}

	if !noCommit {
		opts := wkcmd.AmbientCommitOptions(ctx, campaignRoot, cfg.ID, os.Stderr)
		opts.Files = commit.NormalizeFiles(campaignRoot, result.Path, audit.FilePath(resolver.Intents()))
		opts.SelectiveOnly = true
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options:     opts,
			Action:      commit.IntentRelease,
			IntentTitle: result.Title,
			Description: fmt.Sprintf("Released from %s", previousAssignee),
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
		commit.WarnIfSkipped(os.Stderr, commitResult)
	}

	return nil
}
