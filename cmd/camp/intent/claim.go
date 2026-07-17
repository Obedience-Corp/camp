package intent

import (
	"fmt"
	"os"
	"strings"

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

var intentClaimCmd = &cobra.Command{
	Use:   "claim <id>",
	Short: "Claim an intent for an agent or session",
	Long: `Assign an intent to an agent so the campaign tracks who is working it.

Stamps assigned_to and assigned_at, and merges any --ref values (a PR URL,
branch, or festival path) into work_ref. Calling claim again on an
already-claimed intent re-stamps assigned_at and merges in new refs without
dropping ones already recorded -- this is the expected way to record a PR URL
once one is opened, after an initial claim at the start of work.

Use 'camp intent release' to clear the assignment, and 'camp intent sync' to
auto-close intents once their tracked PR merges.

Examples:
  camp intent claim add-dark --agent claude-code-session-1
  camp intent claim add-dark --agent claude-code-session-1 \
    --ref https://github.com/Obedience-Corp/camp/pull/123`,
	Args: cobra.ExactArgs(1),
	RunE: runIntentClaim,
}

func init() {
	Cmd.AddCommand(intentClaimCmd)

	flags := intentClaimCmd.Flags()
	flags.String("agent", "", "Agent or session name claiming the intent (required)")
	flags.StringArray("ref", nil, "Work reference: PR URL, branch, or festival path (repeatable)")
	flags.Bool("no-commit", false, "Don't create a git commit")
}

func runIntentClaim(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]

	agent, _ := cmd.Flags().GetString("agent")
	agent = strings.TrimSpace(agent)
	refs, _ := cmd.Flags().GetStringArray("ref")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	if agent == "" {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--agent is required")
	}

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
	previousAssignee := before.AssignedTo

	result, err := svc.Claim(ctx, id, intent.ClaimOptions{Agent: agent, Refs: refs})
	if err != nil {
		return camperrors.Wrap(err, "failed to claim intent")
	}

	if previousAssignee != "" && previousAssignee != agent {
		fmt.Printf("✓ Intent reclaimed from %s by %s: %s\n", previousAssignee, agent, result.Path)
	} else {
		fmt.Printf("✓ Intent claimed by %s: %s\n", agent, result.Path)
	}

	if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
		Type:   audit.EventClaim,
		ID:     result.ID,
		Title:  result.Title,
		To:     string(result.Status),
		Reason: fmt.Sprintf("assigned to %s", agent),
	}); err != nil {
		return err
	}

	if !noCommit {
		opts := wkcmd.AmbientCommitOptions(ctx, campaignRoot, cfg.ID, os.Stderr)
		opts.Files = commit.NormalizeFiles(campaignRoot, result.Path, audit.FilePath(resolver.Intents()))
		opts.SelectiveOnly = true
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options:     opts,
			Action:      commit.IntentClaim,
			IntentTitle: result.Title,
			Description: fmt.Sprintf("Claimed by %s", agent),
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
		commit.WarnIfSkipped(os.Stderr, commitResult)
	}

	return nil
}
