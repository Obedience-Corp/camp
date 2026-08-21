package workitem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ledger"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// sweepAnswer is one answer to the per-workitem question.
type sweepAnswer string

const (
	answerRouteDocs sweepAnswer = "docs"
	answerCompleted sweepAnswer = "completed"
	answerSomeday   sweepAnswer = "someday"
	answerArchived  sweepAnswer = "archived"
	answerKeep      sweepAnswer = "keep"
)

// runSweepPrompt asks per workitem and acts on the answer. Nothing moves without
// one: a declined item, a cancelled form, and a form that failed to run all leave
// the workitem exactly where it was.
//
// The skips are printed first. They are decisions camp already made on the
// user's behalf, and they belong in front of the questions rather than after
// them, where they would read as an afterthought.
func runSweepPrompt(ctx context.Context, cmd *cobra.Command, work *sweepWork) error {
	out := cmd.OutOrStdout()
	if err := printWorkSkips(out, work); err != nil {
		return err
	}
	if len(work.actionable) == 0 {
		return nil
	}

	for i, cand := range work.actionable {
		if err := ctx.Err(); err != nil {
			return err
		}
		answer, cancelled, err := askSweepAnswer(ctx, work, cand)
		if err != nil {
			_, _ = fmt.Fprintf(out, "%s prompt failed for %s: %v\n",
				ui.WarningIcon(), cand.Item.RelativePath, err)
			continue
		}
		// Cancelling one question ends the run. Asking the remaining questions
		// after someone pressed Ctrl+C would make the only escape from a long
		// queue be pressing it once per item.
		if cancelled {
			if remaining := len(work.actionable) - i; remaining > 0 {
				_, _ = fmt.Fprintf(out, "  %s cancelled; %d workitem(s) left untouched\n",
					ui.InfoIcon(), remaining)
			}
			return nil
		}
		if err := applySweepAnswer(ctx, cmd, work, cand, answer); err != nil {
			if ctx.Err() != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "%s %s: %v\n", ui.WarningIcon(), cand.Item.RelativePath, err)
		}
	}
	return nil
}

func printWorkSkips(out io.Writer, work *sweepWork) error {
	for _, skip := range work.skipped {
		if _, err := fmt.Fprintf(out, "  %s %s (%s): not moved, %s\n",
			ui.InfoIcon(), filepath.ToSlash(skip.Item.RelativePath), skip.Item.WorkflowType, skip.Detail); err != nil {
			return err
		}
	}
	return nil
}

// askSweepAnswer runs the question for one candidate. Explore and research items
// get the full set of destinations because "the loop finished" does not say where
// the findings belong; every other type gets the same Promote/Skip confirm the
// merged-branch backstop uses, since for those the loop was the work.
//
// The second return value reports a cancelled form (Ctrl+C) separately from a
// declined one: declining means "not this workitem", cancelling means "stop".
func askSweepAnswer(ctx context.Context, work *sweepWork, cand wkitem.SweepCandidate) (sweepAnswer, bool, error) {
	title := sweepPromptTitle(cand)
	description := sweepPromptDescription(work, cand)

	if cand.Disposition != wkitem.DispositionRoute {
		var promote bool
		form := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().Title(title).Description(description).
				Affirmative("Promote").Negative("Skip").Value(&promote),
		))
		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return answerKeep, true, nil
			}
			return answerKeep, false, err
		}
		if promote {
			return answerCompleted, false, nil
		}
		return answerKeep, false, nil
	}

	// Options before Value, and not the other way around. huh's Select resolves
	// the pointer's current value against the option list at the moment Value is
	// called, so setting it first leaves the list unresolved and the form renders
	// with a single row where five belong. That is invisible to a unit test and
	// was caught by tests/tui/sweep_prompt_pty.py.
	answer := answerKeep
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[sweepAnswer]().
			Title(title).
			Description(description).
			Options(
				huh.NewOption("Route to docs - move the findings into a docs/ subdirectory", answerRouteDocs),
				huh.NewOption("Mark completed - shelve it in dungeon/completed", answerCompleted),
				huh.NewOption("Someday - shelve it in dungeon/someday", answerSomeday),
				huh.NewOption("Archive - shelve it in dungeon/archived", answerArchived),
				huh.NewOption("Keep working - leave it where it is", answerKeep),
			).
			Value(&answer),
	))
	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return answerKeep, true, nil
		}
		return answerKeep, false, err
	}
	return answer, false, nil
}

func sweepPromptTitle(cand wkitem.SweepCandidate) string {
	label := cand.Item.Title
	if label == "" {
		label = wkitem.LinkWorkitemID(&cand.Item)
	}
	if cand.Disposition == wkitem.DispositionRoute {
		return fmt.Sprintf("Workflow run completed for %q. Where do the findings go?", label)
	}
	return fmt.Sprintf("Workflow run completed for %q. Promote to completed?", label)
}

// sweepPromptDescription identifies the workitem beyond its title so the decision
// can be made from the prompt alone, and names the links scoped inside it,
// because accepting releases them.
func sweepPromptDescription(work *sweepWork, cand wkitem.SweepCandidate) string {
	lines := []string{"ID:        " + wkitem.LinkWorkitemID(&cand.Item)}
	if cand.Item.RelativePath != "" {
		lines = append(lines, "Directory: "+filepath.ToSlash(cand.Item.RelativePath))
	}
	if cand.RunID != "" {
		lines = append(lines, "Run:       "+cand.RunID+" (completed)")
	}
	for _, l := range work.linkedScopes[cand.Item.RelativePath] {
		lines = append(lines, fmt.Sprintf("Link:      %s (%s:%s) is released if this moves",
			l.ID, l.Scope.Kind, l.Scope.Path))
	}
	return strings.Join(lines, "\n")
}

// applySweepAnswer performs the answer. Every shelving answer goes through
// sweepOneToStatus, the same move + audit + ledger + link-release + commit
// sequence the automatic path uses, so a prompted move and a swept move cannot
// diverge.
func applySweepAnswer(ctx context.Context, cmd *cobra.Command, work *sweepWork, cand wkitem.SweepCandidate, answer sweepAnswer) error {
	out := cmd.OutOrStdout()
	switch answer {
	case answerKeep:
		_, err := fmt.Fprintf(out, "  %s %s left in place\n", ui.InfoIcon(), cand.Item.RelativePath)
		return err
	case answerRouteDocs:
		return routeCandidateToDocs(ctx, cmd, work, cand)
	case answerCompleted, answerSomeday, answerArchived:
		var result workitemSweepResult
		entry := sweepOneToStatus(ctx, cmd, work.cfg, work.root, cand, string(answer), &result)
		if entry.Error != "" {
			return camperrors.New(entry.Error)
		}
		_, err := fmt.Fprintf(out, "  %s %s -> %s\n", ui.SuccessIcon(), entry.From, entry.To)
		return err
	default:
		return camperrors.New("unhandled sweep answer: " + string(answer))
	}
}

// routeCandidateToDocs moves the workitem directory into a docs/ subdirectory the
// user picks, then releases its links and commits both sides of the move.
//
// The picker and the move are the dungeon crawl's route-to-docs action
// (dungeon.PromptDocsDestination and Service.MoveToDocs), reused rather than
// reimplemented so docs routing has one set of boundary rules no matter which
// command asked for it.
func routeCandidateToDocs(ctx context.Context, cmd *cobra.Command, work *sweepWork, cand wkitem.SweepCandidate) error {
	out := cmd.OutOrStdout()
	loc, err := resolveSweepLocation(work.root, cand.Item)
	if err != nil {
		return err
	}
	fromRel := filepath.ToSlash(dungeoncmd.RelFromRoot(work.root, loc.SourcePath))

	destination, err := intdungeon.PromptDocsDestination(ctx, loc.Slug, work.root)
	if err != nil {
		if errors.Is(err, intdungeon.ErrCrawlAborted) {
			_, ferr := fmt.Fprintf(out, "  %s %s left in place\n", ui.InfoIcon(), fromRel)
			return ferr
		}
		return err
	}
	if destination == "" {
		_, ferr := fmt.Fprintf(out, "  %s %s left in place\n", ui.InfoIcon(), fromRel)
		return ferr
	}

	// Identity has to be read before the move: the marker travels with the
	// directory, so loc.SourcePath stops resolving the moment it lands in docs/.
	ident := sweptWorkitemIdentity(ctx, loc, cand.Item)
	oldKey := cand.Item.Key
	if oldKey == "" {
		oldKey = string(cand.Item.WorkflowType) + ":" + fromRel
	}

	svc := intdungeon.NewService(work.root, loc.DungeonPath)
	targetPath, err := svc.MoveToDocs(ctx, loc.Slug, loc.ParentPath, destination)
	if err != nil {
		return camperrors.Wrapf(err, "routing %s to docs/%s", loc.Slug, destination)
	}
	toRel := filepath.ToSlash(dungeoncmd.RelFromRoot(work.root, targetPath))

	// Routing to docs ends the workitem's active life exactly as a shelve does:
	// nothing should still be checked out "for" work that is now reference
	// material, so its links go with it and are reported.
	dropped, err := unlinkShelvedWorkitem(ctx, work.root, ident.LinkID, oldKey, fromRel)
	if err != nil {
		return camperrors.Wrap(err, "release workitem links on route to docs")
	}

	appendWorkitemAuditEvent(ctx, cmd, work.root, wkaudit.Event{
		Event:    wkaudit.EventPromote,
		ID:       ident.LedgerID,
		Ref:      ident.LedgerRef,
		Title:    ident.LedgerTitle,
		Type:     string(cand.Item.WorkflowType),
		From:     fromRel,
		To:       toRel,
		Target:   "doc",
		Evidence: cand.Reason,
	})
	ledger.NewFromRoot(ctx, work.root, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindTransitioned, ledgerkit.Scope{Workitem: ident.LedgerID},
			ledger.WithWhy("sweep route to docs"),
			ledger.WithPayload(map[string]any{
				"target": "doc", "from": fromRel, "to": toRel,
				"evidence": cand.Reason, "run_id": cand.RunID,
			}))

	destPaths := []string{targetPath, filepath.Join(work.root, ".campaign", "workitems", wkaudit.AuditFile)}
	if len(dropped) > 0 {
		destPaths = append(destPaths, links.LinksPath(work.root))
		for _, l := range dropped {
			_, _ = fmt.Fprintf(out, "  released link %s (%s:%s); %s is no longer active\n",
				l.ID, l.ScopeKind, l.ScopePath, ident.LedgerID)
		}
	}
	outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
		Config:           work.cfg,
		CampaignRoot:     work.root,
		Description:      fmt.Sprintf("Route workitem %s to %s (sweep: %s)", loc.Slug, toRel, cand.Reason),
		SourcePaths:      []string{loc.SourcePath},
		DestinationPaths: destPaths,
		RewrittenFiles:   svc.RewrittenLinkFiles(),
	})
	if cerr := outcome.Err(); cerr != nil {
		return camperrors.Wrapf(cerr, "committing route of %s to %s", loc.Slug, toRel)
	}
	if navErr := navindex.Delete(work.root); navErr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s failed to invalidate navigation cache after %s: %v\n",
			ui.WarningIcon(), ident.LedgerID, navErr)
	}
	_, err = fmt.Fprintf(out, "  %s %s -> %s\n", ui.SuccessIcon(), fromRel, toRel)
	return err
}
