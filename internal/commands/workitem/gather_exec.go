package workitem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

type gatherExecution struct {
	ID            string
	Ref           string
	Moves         []workitemGatherResultMove
	Warnings      []string
	Committed     bool
	CommitMessage string
}

// executeGather applies a validated gather plan: it creates the gathered
// package, moves each source directory inside it, stamps gather lineage,
// migrates priority and link state, appends audit events, and commits.
// Bookkeeping failures after the moves are reported as warnings rather than
// errors so a partial failure never strands the filesystem mid-operation.
func executeGather(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, plan gatherPlan, opts gatherOptions, warnings []string) (*gatherExecution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	execution := &gatherExecution{Warnings: warnings}

	id, err := generateID(ctx, plan.WorkflowType, plan.Slug, "", root)
	if err != nil {
		return nil, err
	}
	ref, err := deriveUniqueRef(ctx, root, cfg, id)
	if err != nil {
		return nil, err
	}
	execution.ID, execution.Ref = id, ref

	if err := os.MkdirAll(plan.TargetAbs, 0o755); err != nil {
		return nil, camperrors.Wrap(err, "create gathered directory")
	}
	created := false
	defer func() {
		// Only remove the target while it holds nothing but our own files;
		// once created flips true the source moves may have started.
		if !created {
			_ = os.RemoveAll(plan.TargetAbs)
		}
	}()

	meta := wkitem.Metadata{
		Version: wkitem.WorkitemSchemaVersion,
		Kind:    wkitem.MetadataKind,
		ID:      id,
		Type:    plan.WorkflowType,
		Title:   plan.Title,
		Ref:     ref,
	}
	buf, err := yaml.Marshal(&meta)
	if err != nil {
		return nil, camperrors.Wrap(err, "marshal metadata")
	}
	if err := fsutil.WriteFileAtomically(filepath.Join(plan.TargetAbs, wkitem.MetadataFilename), buf, 0o644); err != nil {
		return nil, err
	}

	sourceItems := make([]wkitem.WorkItem, 0, len(plan.Sources))
	for _, src := range plan.Sources {
		sourceItems = append(sourceItems, src.Item)
	}
	readme := gatherReadme(plan.Title, plan.WorkflowType, sourceItems)
	if err := fsutil.WriteFileAtomically(filepath.Join(plan.TargetAbs, "README.md"), []byte(readme), 0o644); err != nil {
		return nil, err
	}
	created = true

	now := time.Now().UTC()
	idMap := make(map[string]string)
	sourceAbs := make([]string, 0, len(plan.Sources))
	sourceKeys := make([]string, 0, len(plan.Sources))
	for _, src := range plan.Sources {
		base := filepath.Base(src.Item.RelativePath)
		fromAbs := src.Item.AbsPath(root)
		toAbs := filepath.Join(plan.TargetAbs, base)
		if err := os.Rename(fromAbs, toAbs); err != nil {
			moved := make([]string, 0, len(execution.Moves))
			for _, m := range execution.Moves {
				moved = append(moved, m.Slug)
			}
			detail := "none"
			if len(moved) > 0 {
				detail = strings.Join(moved, ", ")
			}
			return nil, camperrors.Wrapf(err, "moving %s into %s (already moved: %s; recover with git status)", base, plan.TargetRel, detail)
		}

		toRel := plan.TargetRel + "/" + base
		move := workitemGatherResultMove{
			Slug: base,
			From: filepath.ToSlash(src.Item.RelativePath),
			To:   toRel,
		}
		if src.Meta != nil && src.Meta.ID != "" {
			move.ID = src.Meta.ID
			idMap[src.Meta.ID] = id
		}
		execution.Moves = append(execution.Moves, move)
		sourceAbs = append(sourceAbs, fromAbs)
		sourceKeys = append(sourceKeys, src.Item.Key)

		if src.Meta != nil {
			if err := wkitem.RecordGather(ctx, root, toRel, id, now); err != nil {
				execution.Warnings = append(execution.Warnings, fmt.Sprintf("stamp gathered_into on %s: %v", base, err))
			}
		}
	}

	targetKey := plan.WorkflowType + ":" + plan.TargetRel
	if err := priority.WithLock(ctx, priority.StorePath(root), func(store *priority.Store) error {
		migrateGatherPriorities(store, sourceKeys, targetKey)
		return nil
	}); err != nil {
		execution.Warnings = append(execution.Warnings, fmt.Sprintf("migrate priority entries: %v", err))
	}

	linksChanged := false
	if len(idMap) > 0 {
		if err := links.WithLock(ctx, root, func(reg *links.Links) error {
			if !rehomeGatherLinks(reg, idMap) {
				return links.ErrSkipSave
			}
			linksChanged = true
			return nil
		}); err != nil {
			execution.Warnings = append(execution.Warnings, fmt.Sprintf("re-home workitem links: %v", err))
		}

		cur, curErr := links.LoadCurrent(ctx, root)
		switch {
		case curErr != nil:
			execution.Warnings = append(execution.Warnings, fmt.Sprintf("read current workitem selection: %v", curErr))
		case cur != nil:
			if _, ok := idMap[cur.WorkitemID]; ok {
				cur.WorkitemID = id
				if err := links.SaveCurrent(ctx, root, cur); err != nil {
					execution.Warnings = append(execution.Warnings, fmt.Sprintf("update current workitem selection: %v", err))
				}
			}
		}
	}

	for _, move := range execution.Moves {
		eventID := move.ID
		if eventID == "" {
			eventID = move.Slug
		}
		if err := wkaudit.AppendEvent(ctx, root, wkaudit.Event{
			Event:        wkaudit.EventGather,
			ID:           eventID,
			Type:         plan.WorkflowType,
			From:         move.From,
			To:           move.To,
			GatheredInto: id,
		}); err != nil {
			execution.Warnings = append(execution.Warnings, fmt.Sprintf("write gather audit event for %s: %v", eventID, err))
		}
	}
	if err := wkaudit.AppendEvent(ctx, root, wkaudit.Event{
		Event: wkaudit.EventGather,
		ID:    id,
		Type:  plan.WorkflowType,
		To:    plan.TargetRel,
	}); err != nil {
		execution.Warnings = append(execution.Warnings, fmt.Sprintf("write gather audit event for %s: %v", id, err))
	}

	if navErr := navindex.Delete(root); navErr != nil {
		execution.Warnings = append(execution.Warnings, fmt.Sprintf("failed to invalidate navigation cache: %v", navErr))
	}

	if !opts.NoCommit {
		// The priority store and current.yaml are gitignored per-machine
		// state, so only the gathered package, audit log, and (when changed)
		// the shared links registry are staged.
		dest := []string{
			plan.TargetAbs,
			filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile),
		}
		if linksChanged {
			dest = append(dest, links.LinksPath(root))
		}
		preStaged, stageErr := dungeoncmd.StageTrackedMoveSourceDeletions(ctx, root, sourceAbs)
		if stageErr != nil {
			return nil, camperrors.Wrap(stageErr, "staging move source deletions (gather was applied on disk; recover with git status)")
		}
		sourceSlugs := make([]string, 0, len(execution.Moves))
		for _, move := range execution.Moves {
			sourceSlugs = append(sourceSlugs, move.Slug)
		}
		result := commit.Workitem(ctx, commit.WorkitemOptions{
			Options: commit.Options{
				CampaignRoot:  root,
				CampaignID:    cfg.ID,
				Files:         commit.NormalizeFiles(root, dest...),
				PreStaged:     preStaged,
				SelectiveOnly: true,
			},
			Action:      commit.WorkitemGather,
			WorkitemID:  id,
			WorkitemRef: ref,
			Title:       fmt.Sprintf("Gather %d %s workitems into %s", len(plan.Sources), plan.WorkflowType, plan.TargetRel),
			Detail:      "Sources: " + strings.Join(sourceSlugs, ", "),
		})
		if !opts.JSON && result.Message != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", result.Message)
		}
		execution.Committed = result.Committed
		execution.CommitMessage = result.Message
		if result.Err != nil {
			return nil, camperrors.Wrap(result.Err, "auto-committing gather (gather was applied on disk)")
		}
	}

	return execution, nil
}
