package workitem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// applyConvert performs the on-disk type-root move, updates the marker type,
// repairs every reference, and commits the result. The physical move runs
// first; bookkeeping that fails afterward is surfaced as a warning rather
// than stranding the already-moved workitem — except marker type, which is
// part of the convert contract and fails the command (recover with git status).
func applyConvert(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, plan *convertPlan, opts runWorkitemConvertOptions, result workitemConvertResult) error {
	warn := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		result.Warnings = append(result.Warnings, msg)
		if !opts.JSON {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", ui.WarningIcon(), msg)
		}
	}

	rewritten, err := renameMove(ctx, root, plan.asRenamePlan())
	if err != nil {
		return err
	}
	result.RewrittenFiles = rewritten

	markerUpdated, err := updateConvertedType(ctx, cfg, root, plan)
	if err != nil {
		return camperrors.Wrapf(err, "updating marker type after converting %s (move applied; recover with git status)", filepath.Base(plan.oldRel))
	}
	result.MarkerUpdated = markerUpdated

	destPaths := []string{plan.dstPath}
	if p, ok := stageConvertAuditEvent(ctx, cmd, root, plan); ok {
		destPaths = append(destPaths, p)
	}

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
	if linksChanged {
		destPaths = append(destPaths, links.LinksPath(root))
	}

	if navErr := navindex.Delete(root); navErr != nil {
		warn("failed to invalidate navigation cache: %v", navErr)
	}

	if !opts.NoCommit {
		if err := commitConvert(ctx, cmd, cfg, root, plan, opts, destPaths, rewritten, &result); err != nil {
			return err
		}
	}

	return emitConvertOutcome(cmd, opts, result)
}

// updateConvertedType writes the destination type onto the moved workitem.
// Directory markers are updated in place; a missing marker is created only
// when the destination type is custom (builtin design/explore remain
// discoverable by path). File workitems stamp frontmatter type.
func updateConvertedType(ctx context.Context, cfg *config.CampaignConfig, root string, plan *convertPlan) (bool, error) {
	if plan.isFile {
		return true, wkitem.StampFrontmatterFields(ctx, plan.dstPath, []wkitem.FrontmatterField{
			{After: "id", Key: "type", Value: plan.newType},
		})
	}

	markerAbs := filepath.Join(plan.dstPath, wkitem.MetadataFilename)
	raw, err := os.ReadFile(markerAbs)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, camperrors.Wrapf(err, "read %s", markerAbs)
		}
		if wkitem.IsBuiltinDocType(wkitem.WorkflowType(plan.newType)) {
			return false, nil
		}
		repair, rerr := planRepair(ctx, root, cfg, filepath.ToSlash(plan.newRel), plan.newType)
		if rerr != nil {
			return false, rerr
		}
		if err := writeMarker(plan.dstPath, repair.meta); err != nil {
			return false, err
		}
		return true, nil
	}

	var meta wkitem.Metadata
	if uerr := yaml.Unmarshal(raw, &meta); uerr != nil {
		return false, camperrors.NewValidation("marker",
			"cannot parse existing .workitem as YAML ("+uerr.Error()+"); fix or remove it manually before converting", nil)
	}
	if meta.Type == plan.newType {
		return false, nil
	}
	meta.Type = plan.newType
	if err := writeMarker(plan.dstPath, meta); err != nil {
		return false, err
	}
	return true, nil
}

func stageConvertAuditEvent(ctx context.Context, cmd *cobra.Command, root string, plan *convertPlan) (string, bool) {
	eventID := plan.item.StableID
	if eventID == "" {
		eventID = filepath.Base(plan.newRel)
	}
	appendWorkitemAuditEvent(ctx, cmd, root, wkaudit.Event{
		Event: wkaudit.EventMove,
		ID:    eventID,
		Ref:   refFromItem(plan.asRenamePlan()),
		Type:  plan.newType,
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

func commitConvert(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, plan *convertPlan, opts runWorkitemConvertOptions, destPaths, rewritten []string, result *workitemConvertResult) error {
	outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
		Config:           cfg,
		CampaignRoot:     root,
		Description:      fmt.Sprintf("Convert workitem %s from %s to %s", filepath.Base(plan.oldRel), plan.oldType, plan.newType),
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

func emitConvertOutcome(cmd *cobra.Command, opts runWorkitemConvertOptions, result workitemConvertResult) error {
	if opts.JSON {
		return emitConvertJSON(cmd.OutOrStdout(), result)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s Converted workitem %s from %s to %s\n",
		ui.SuccessIcon(), result.From, result.FromType, result.ToType)
	return err
}
