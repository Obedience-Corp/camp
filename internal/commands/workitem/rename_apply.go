package workitem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

// applyRename performs the on-disk rename, repairs every reference, and commits
// the result. The physical move runs first; bookkeeping that fails afterward is
// surfaced as a warning rather than stranding the already-moved workitem.
func applyRename(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, plan *renamePlan, opts runWorkitemRenameOptions, result workitemRenameResult) error {
	warn := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		result.Warnings = append(result.Warnings, msg)
		if !opts.JSON {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", ui.WarningIcon(), msg)
		}
	}

	rewritten, err := renameMove(ctx, root, plan)
	if err != nil {
		return err
	}
	result.RewrittenFiles = rewritten

	destPaths := []string{plan.dstPath}
	if p, ok := stageAuditEvent(ctx, cmd, root, plan); ok {
		destPaths = append(destPaths, p)
	}

	linksChanged := migrateRenameReferences(ctx, root, plan, &result, warn)
	if linksChanged {
		destPaths = append(destPaths, links.LinksPath(root))
	}

	if navErr := navindex.Delete(root); navErr != nil {
		warn("failed to invalidate navigation cache: %v", navErr)
	}

	if !opts.NoCommit {
		if err := commitRename(ctx, cmd, cfg, root, plan, opts, destPaths, rewritten, &result); err != nil {
			return err
		}
	}

	return emitRenameOutcome(cmd, opts, result)
}

// renameMove applies the physical basename change and rewrites the markdown
// links that referenced the moved path.
func renameMove(ctx context.Context, root string, plan *renamePlan) ([]string, error) {
	if _, err := statusmove.Move(ctx, plan.srcPath, plan.dstPath, statusmove.MoveOptions{BoundaryRoot: root}); err != nil {
		if errors.Is(err, statusmove.ErrAlreadyExists) {
			return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists,
				"cannot rename %s: destination %s already exists",
				filepath.Base(plan.oldRel), filepath.ToSlash(plan.newRel))
		}
		return nil, camperrors.Wrapf(err, "renaming %s to %s", filepath.ToSlash(plan.oldRel), filepath.ToSlash(plan.newRel))
	}
	rewritten, err := mdlinks.RewriteForMove(ctx, root, plan.srcPath, plan.dstPath)
	if err != nil {
		return nil, camperrors.Wrapf(err, "rewriting markdown links after renaming %s (move applied; recover with git status)", filepath.Base(plan.oldRel))
	}
	return rewritten, nil
}

// migrateRenameReferences repairs the priority store, link registry, and
// current-workitem pointer for the new path. It returns whether links.yaml
// changed so the caller can stage it. Each store is best-effort: a failure is
// recorded as a warning without failing the already-applied move.
func migrateRenameReferences(ctx context.Context, root string, plan *renamePlan, result *workitemRenameResult, warn func(string, ...any)) bool {
	if moved, err := migrateRenamePriority(ctx, root, plan.oldKey, plan.newKey); err != nil {
		warn("migrate priority entries: %v", err)
	} else {
		result.PriorityMoved = moved
	}

	linksChanged, err := migrateRenameLinks(ctx, root, plan.oldKey, plan.newKey, plan.oldRel, plan.newRel)
	if err != nil {
		warn("re-home workitem links: %v", err)
	}
	result.LinksUpdated = linksChanged

	if updated, err := migrateRenameCurrent(ctx, root, plan.oldKey, plan.newKey); err != nil {
		warn("update current workitem selection: %v", err)
	} else {
		result.CurrentUpdated = updated
	}
	return linksChanged
}

// migrateRenamePriority re-keys the manual priority and attention entries. The
// store at .campaign/settings/workitems.json is gitignored per-machine state,
// so it is migrated on disk and never staged into the rename commit (matching
// gather's treatment of the same file).
func migrateRenamePriority(ctx context.Context, root, oldKey, newKey string) (bool, error) {
	path := priority.StorePath(root)
	store, err := priority.Load(path)
	if err != nil {
		return false, err
	}
	if _, ok := store.ManualPriorities[oldKey]; !ok {
		if _, ok := store.Attention[oldKey]; !ok {
			return false, nil
		}
	}
	changed := false
	if err := priority.WithLock(ctx, path, func(s *priority.Store) error {
		changed = rekeyRenamePriorities(s, oldKey, newKey)
		return nil
	}); err != nil {
		return false, err
	}
	return changed, nil
}

// migrateRenameLinks rewrites the shared link registry under lock. links.yaml is
// tracked, so a change is reported for staging.
func migrateRenameLinks(ctx context.Context, root, oldKey, newKey, oldRel, newRel string) (bool, error) {
	changed := false
	if err := links.WithLock(ctx, root, func(reg *links.Links) error {
		if !rehomeRenameLinks(reg, oldKey, newKey, oldRel, newRel) {
			return links.ErrSkipSave
		}
		changed = true
		return nil
	}); err != nil {
		return false, err
	}
	return changed, nil
}

// migrateRenameCurrent updates current.yaml only when it referenced the old
// path key. A pointer stored as a stable id needs no change because the rename
// preserves the id; current.yaml is gitignored and is not staged.
func migrateRenameCurrent(ctx context.Context, root, oldKey, newKey string) (bool, error) {
	cur, err := links.LoadCurrent(ctx, root)
	if err != nil {
		return false, err
	}
	if cur == nil || cur.WorkitemID != oldKey {
		return false, nil
	}
	cur.WorkitemID = newKey
	if err := links.SaveCurrent(ctx, root, cur); err != nil {
		return false, err
	}
	return true, nil
}

// stageAuditEvent appends the workitem-level move event and returns the ledger
// path when it exists on disk, so the caller stages it into the same commit.
// git add hard-fails on a never-created path, so an unwritten ledger is skipped.
func stageAuditEvent(ctx context.Context, cmd *cobra.Command, root string, plan *renamePlan) (string, bool) {
	eventID := plan.item.StableID
	if eventID == "" {
		eventID = filepath.Base(plan.newRel)
	}
	appendWorkitemAuditEvent(ctx, cmd, root, wkaudit.Event{
		Event: wkaudit.EventMove,
		ID:    eventID,
		Ref:   refFromItem(plan),
		Type:  string(plan.item.WorkflowType),
		Title: plan.item.Title,
		From:  filepath.ToSlash(plan.oldRel),
		To:    filepath.ToSlash(plan.newRel),
	})
	path := filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, true
	}
	return "", false
}

func refFromItem(plan *renamePlan) string {
	if plan.item.SourceMetadata == nil {
		return ""
	}
	if ref, ok := plan.item.SourceMetadata["ref"].(string); ok {
		return ref
	}
	return ""
}

// commitRename stages the move (recording the source deletion as a rename) and
// auto-commits it alongside every repaired reference.
func commitRename(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, plan *renamePlan, opts runWorkitemRenameOptions, destPaths, rewritten []string, result *workitemRenameResult) error {
	outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
		Config:           cfg,
		CampaignRoot:     root,
		Description:      fmt.Sprintf("Rename workitem %s to %s", filepath.Base(plan.oldRel), filepath.Base(plan.newRel)),
		SourcePaths:      []string{plan.srcPath},
		DestinationPaths: destPaths,
		RewrittenFiles:   rewritten,
	})
	if !opts.JSON {
		dungeoncmd.PrintDungeonMoveOutcome(cmd.OutOrStdout(), outcome)
	}
	result.Committed = outcome.Committed
	result.CommitMessage = outcome.Message
	return outcome.Err()
}

func emitRenameOutcome(cmd *cobra.Command, opts runWorkitemRenameOptions, result workitemRenameResult) error {
	if opts.JSON {
		return emitRenameJSON(cmd.OutOrStdout(), result)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s Renamed workitem %s to %s\n",
		ui.SuccessIcon(), result.From, result.To)
	return err
}
