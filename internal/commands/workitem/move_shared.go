package workitem

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/mdlinks"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/statusmove"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// workitemMove describes one physical relocation of a workitem directory that
// keeps it active: onto a rail stage, or off the rail back to its type root.
// Shelving is not one of these, because it releases path-keyed state instead of
// re-homing it.
type workitemMove struct {
	SourcePath  string
	DestPath    string
	OldRel      string
	NewRel      string
	OldKey      string
	NewKey      string
	Description string
}

// applyWorkitemMove performs the move and repairs every reference to it,
// returning commitInputs for the shared tail. Used by the rail move and by
// demote so the two cannot drift.
func applyWorkitemMove(ctx context.Context, root string, mv workitemMove, result *workitemPromoteResult) (*commitInputs, error) {
	if _, err := statusmove.Move(ctx, mv.SourcePath, mv.DestPath, statusmove.MoveOptions{BoundaryRoot: root}); err != nil {
		if errors.Is(err, statusmove.ErrAlreadyExists) {
			return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists,
				"cannot move %s: destination %s already exists", mv.OldRel, mv.NewRel)
		}
		return nil, camperrors.Wrapf(err, "moving %s to %s", mv.OldRel, mv.NewRel)
	}
	rewritten, err := mdlinks.RewriteForMove(ctx, root, mv.SourcePath, mv.DestPath)
	if err != nil {
		return nil, camperrors.Wrapf(err,
			"rewriting markdown links after moving %s (move applied; recover with git status)", mv.OldRel)
	}

	destPaths := []string{mv.DestPath}
	if migrateRailReferences(ctx, root, mv.OldKey, mv.NewKey, mv.OldRel, mv.NewRel, result) {
		destPaths = append(destPaths, links.LinksPath(root))
	}

	result.To = mv.NewRel
	return &commitInputs{
		description: mv.Description,
		sourcePaths: []string{mv.SourcePath},
		destPaths:   destPaths,
		rewritten:   rewritten,
	}, nil
}

// moveTailOptions carries the flags the shared tail honors.
type moveTailOptions struct {
	NoCommit bool
	JSON     bool
}

// moveTail describes how one move should be recorded and reported. Grouped into
// a struct because five consecutive string parameters can be transposed without
// the compiler noticing.
type moveTail struct {
	LedgerID    string
	LedgerRef   string
	LedgerTitle string
	Why         string
	SuccessVerb string
	Options     moveTailOptions
}

// finishWorkitemMove appends the audit event, emits the ledger transition,
// auto-commits, invalidates the nav cache, and prints the outcome. Shared by
// promote and demote so their event streams and commit shape stay identical.
func finishWorkitemMove(
	ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string,
	ci *commitInputs, result *workitemPromoteResult, tail moveTail,
) error {
	opts := tail.Options
	appendWorkitemAuditEvent(ctx, cmd, root, wkaudit.Event{
		Event:      wkaudit.EventPromote,
		ID:         tail.LedgerID,
		Ref:        tail.LedgerRef,
		Title:      tail.LedgerTitle,
		Type:       result.Type,
		From:       result.From,
		To:         result.To,
		Target:     result.Target,
		PromotedTo: result.PromotedTo,
	})
	ci.destPaths = append(ci.destPaths, auditFilePath(root))

	emitTransitionLedger(ctx, cmd, root, result, tail.Why)

	if !opts.NoCommit {
		if err := commitWorkitemMove(ctx, cmd, cfg, root, ci, result, opts.JSON); err != nil {
			return err
		}
	}

	if navErr := navindex.Delete(root); navErr != nil {
		msg := fmt.Sprintf("failed to invalidate navigation cache: %v", navErr)
		result.Warnings = append(result.Warnings, msg)
		if !opts.JSON {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", ui.WarningIcon(), msg)
		}
	}

	if opts.JSON {
		return emitPromoteJSON(cmd, *result)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s workitem %s to %s\n",
		ui.SuccessIcon(), tail.SuccessVerb, result.ID, result.To); err != nil {
		return err
	}
	return printReleasedLinks(cmd.OutOrStdout(), *result)
}

func commitWorkitemMove(
	ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string,
	ci *commitInputs, result *workitemPromoteResult, jsonOut bool,
) error {
	outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
		Config:           cfg,
		CampaignRoot:     root,
		Description:      ci.description,
		SourcePaths:      ci.sourcePaths,
		DestinationPaths: ci.destPaths,
		RewrittenFiles:   ci.rewritten,
	})
	if !jsonOut {
		dungeoncmd.PrintDungeonMoveOutcome(cmd.OutOrStdout(), outcome)
	}
	result.Committed = outcome.Committed
	result.CommitMessage = outcome.Message
	return outcome.Err()
}
