package workitem

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	intaudit "github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func promoteMergedIntent(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, wi wkitem.WorkItem, evidence string) error {
	id := wkitem.LinkWorkitemID(&wi)
	if id == "" {
		return camperrors.New("intent has no resolvable id")
	}
	reason := mergedIntentPromoteReason(evidence)

	resolver := paths.NewResolverFromConfig(root, cfg)
	intentsDir := resolver.Intents()
	svc := intent.NewIntentService(root, intentsDir)
	svc.SetLedger(ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr())))
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring idea directories")
	}

	i, err := svc.Find(ctx, id)
	if err != nil {
		return camperrors.Newf("idea not found: %s", id)
	}
	if i.Status == intent.StatusDone {
		return nil
	}

	intentTitle := i.Title
	sourcePath := i.Path
	prevStatus := i.Status

	intent.AppendDecisionRecord(i, intent.StatusDone, reason)
	if err := svc.Save(ctx, i); err != nil {
		return camperrors.Wrap(err, "failed to save decision record")
	}

	result, err := svc.Move(ctx, id, intent.StatusDone)
	if err != nil {
		return camperrors.Wrap(err, "failed to move idea to done")
	}

	if err := appendIntentAuditEvent(ctx, intentsDir, intaudit.Event{
		Type:   intaudit.EventMove,
		ID:     i.ID,
		Title:  intentTitle,
		From:   string(prevStatus),
		To:     string(intent.StatusDone),
		Reason: reason,
	}); err != nil {
		return err
	}

	opts := AmbientCommitOptions(ctx, root, cfg.ID, cmd.ErrOrStderr())
	opts.Files = commit.NormalizeFiles(root, sourcePath, result.Path, intaudit.FilePath(intentsDir))
	opts.SelectiveOnly = true
	commitResult := commit.Intent(ctx, commit.IntentOptions{
		Options:     opts,
		Action:      commit.IntentMove,
		IntentTitle: intentTitle,
		Description: fmt.Sprintf("Moved to %s status", intent.StatusDone),
	})
	if commitResult.Message != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", commitResult.Message); err != nil {
			return err
		}
	}
	commit.WarnIfSkipped(cmd.ErrOrStderr(), commitResult)
	return nil
}

func mergedIntentPromoteReason(evidence string) string {
	if evidence == wkitem.EvidenceMergedBranch {
		return "merged branch or workitem-tagged commits"
	}
	evidence = strings.TrimSpace(evidence)
	if evidence == "" {
		return "merged branch or workitem-tagged commits"
	}
	return evidence
}

func appendIntentAuditEvent(ctx context.Context, intentsDir string, event intaudit.Event) error {
	if event.Actor == "" {
		event.Actor = resolveIntentActor(ctx)
	}
	if err := intaudit.AppendEvent(ctx, intentsDir, event); err != nil {
		return camperrors.Wrap(err, "writing idea audit event")
	}
	return nil
}

func resolveIntentActor(ctx context.Context) string {
	actor := strings.TrimSpace(git.GetUserName(ctx))
	if actor == "" {
		return "system"
	}
	return actor
}
