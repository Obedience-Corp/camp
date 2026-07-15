package dungeon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/spf13/cobra"
)

type moveFlags struct {
	triage   bool
	toDocs   string
	workitem bool
	dryRun   bool
	jsonOut  bool
}

type movePreview struct {
	Item      string `json:"item"`
	SourceRel string `json:"source"`
	DestRel   string `json:"destination"`
	Status    string `json:"status,omitempty"`
	Mode      string `json:"mode"`
}

type plannedItem struct {
	svc         *intdungeon.Service
	mp          *intdungeon.MovePlan
	description string
	destAbs     string
	preview     movePreview
}

func resolveItemsAndStatus(args []string, f moveFlags) ([]string, string, error) {
	if len(args) == 0 {
		return nil, "", camperrors.New("at least one item is required")
	}
	if f.toDocs != "" || len(args) == 1 {
		return args, "", nil
	}
	return args[:len(args)-1], args[len(args)-1], nil
}

func validateMoveModes(f moveFlags, items []string) error {
	if f.toDocs != "" && !f.triage {
		return camperrors.New("--to-docs requires --triage because docs routing moves parent triage items")
	}
	if f.workitem {
		if f.triage {
			return camperrors.New("--workitem cannot be combined with --triage because workitem mode resolves the source directory itself")
		}
		if f.toDocs != "" {
			return camperrors.New("--workitem cannot be combined with --to-docs")
		}
		if len(items) > 1 {
			return camperrors.New("--workitem accepts a single item per invocation; run it once per workitem")
		}
	}
	if f.jsonOut && !f.dryRun {
		return camperrors.New("--json is only supported with --dry-run; run the move without --json to apply it")
	}
	return nil
}

func runDungeonItemsMove(ctx context.Context, items []string, status string, f moveFlags) error {
	cmdCtx, err := resolveDungeonCommandContext(ctx)
	if err != nil {
		return err
	}
	svc := intdungeon.NewService(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)

	plans, err := planDungeonItems(ctx, svc, cmdCtx, items, status, f)
	if err != nil {
		return err
	}

	if f.dryRun {
		return renderDryRun(os.Stdout, previewsOf(plans), f.jsonOut)
	}
	return applyDungeonMoves(ctx, svc, cmdCtx, plans, status, len(items) > 1)
}

func planDungeonItems(ctx context.Context, svc *intdungeon.Service, cmdCtx *dungeonCommandContext, items []string, status string, f moveFlags) ([]*plannedItem, error) {
	plans := make([]*plannedItem, 0, len(items))
	seenDest := map[string]string{}
	var failures []error

	for _, item := range items {
		pi, err := planDungeonItem(ctx, svc, cmdCtx, item, status, f)
		if err != nil {
			if len(items) == 1 {
				return nil, err
			}
			failures = append(failures, camperrors.Wrap(err, item))
			continue
		}
		if prev, ok := seenDest[pi.destAbs]; ok {
			failures = append(failures, camperrors.New(fmt.Sprintf("%s and %s resolve to the same destination %s", prev, item, pi.preview.DestRel)))
			continue
		}
		seenDest[pi.destAbs] = item
		plans = append(plans, pi)
	}

	if len(failures) > 0 {
		return nil, aggregateMoveErrors(failures)
	}
	return plans, nil
}

func planDungeonItem(ctx context.Context, svc *intdungeon.Service, cmdCtx *dungeonCommandContext, item, status string, f moveFlags) (*plannedItem, error) {
	root := cmdCtx.CampaignRoot
	if f.toDocs != "" {
		mp, err := svc.PlanMoveToDocs(ctx, item, cmdCtx.Dungeon.ParentPath, f.toDocs)
		if err != nil {
			return nil, wrapDungeonDocsRouteError(err, item, f.toDocs)
		}
		return newPlannedItem(svc, mp, root, fmt.Sprintf("Route %s → %s", item, RelFromRoot(root, mp.Destination))), nil
	}

	triage := f.triage
	if !triage {
		inferred, err := inferDungeonMoveTriageMode(ctx, svc, cmdCtx.Dungeon, item)
		if err != nil {
			return nil, err
		}
		triage = inferred
	}

	if triage {
		return planTriageItem(ctx, svc, cmdCtx, item, status)
	}

	if status == "" {
		return nil, camperrors.New("status is required when moving within the dungeon (e.g., completed, archived, someday)")
	}
	mp, err := svc.PlanMoveToStatus(ctx, item, status)
	if err != nil {
		return nil, WrapDungeonMoveError(err, item, status)
	}
	relDir, relErr := filepath.Rel(root, cmdCtx.Dungeon.ParentPath)
	if relErr != nil {
		relDir = cmdCtx.Dungeon.ParentPath
	}
	desc := fmt.Sprintf("Moved to dungeon/%s:\n  - %s/%s", status, relDir, item)
	return newPlannedItem(svc, mp, root, desc), nil
}

func planTriageItem(ctx context.Context, svc *intdungeon.Service, cmdCtx *dungeonCommandContext, item, status string) (*plannedItem, error) {
	root := cmdCtx.CampaignRoot
	if status != "" {
		mp, err := svc.PlanMoveToDungeonStatus(ctx, item, cmdCtx.Dungeon.ParentPath, status)
		if err != nil {
			return nil, WrapDungeonMoveError(err, item, status)
		}
		return newPlannedItem(svc, mp, root, fmt.Sprintf("Triage %s → dungeon/%s", item, status)), nil
	}
	mp, err := svc.PlanMoveToDungeon(ctx, item, cmdCtx.Dungeon.ParentPath)
	if err != nil {
		return nil, WrapDungeonMoveError(err, item, "dungeon")
	}
	return newPlannedItem(svc, mp, root, fmt.Sprintf("Triage %s → dungeon", item)), nil
}

func newPlannedItem(svc *intdungeon.Service, mp *intdungeon.MovePlan, root, description string) *plannedItem {
	return &plannedItem{
		svc:         svc,
		mp:          mp,
		description: description,
		destAbs:     mp.Destination,
		preview: movePreview{
			Item:      mp.ItemName,
			SourceRel: RelFromRoot(root, mp.Source),
			DestRel:   RelFromRoot(root, mp.Destination),
			Status:    mp.Status,
			Mode:      string(mp.Kind),
		},
	}
}

func applyDungeonMoves(ctx context.Context, svc *intdungeon.Service, cmdCtx *dungeonCommandContext, plans []*plannedItem, status string, batch bool) error {
	if batch {
		svc.BeginLinkBatch()
	}

	var sources, dests []string
	var applyErr error
	for _, pi := range plans {
		dst, err := svc.ApplyMove(ctx, pi.mp)
		if err != nil {
			applyErr = camperrors.Wrap(err, pi.preview.Item)
			break
		}
		recordWorkitemMove(ctx, cmdCtx.CampaignRoot, pi.mp.Source, dst)
		fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), pi.preview.Item, pi.preview.SourceRel, pi.preview.DestRel)
		sources = append(sources, pi.mp.Source)
		dests = append(dests, dst)
	}

	if batch {
		if err := svc.FlushLinkRewrites(ctx); err != nil && applyErr == nil {
			applyErr = camperrors.Wrap(err, "rewriting markdown links after moves")
		}
	}

	if len(dests) == 0 {
		return applyErr
	}

	description := plans[0].description
	if batch {
		description = batchCommitDescription(status, plans[:len(dests)])
	}
	if ledgerPath, ok := workitemLedgerPathIfExists(cmdCtx.CampaignRoot); ok {
		dests = append(dests, ledgerPath)
	}
	commitErr := CommitDungeonMove(ctx, &DungeonMoveCommit{
		Config:           cmdCtx.Config,
		CampaignRoot:     cmdCtx.CampaignRoot,
		Description:      description,
		SourcePaths:      sources,
		DestinationPaths: dests,
		RewrittenFiles:   svc.RewrittenLinkFiles(),
	})
	if applyErr != nil {
		return applyErr
	}
	return commitErr
}

func batchCommitDescription(status string, plans []*plannedItem) string {
	var b strings.Builder
	if status != "" {
		fmt.Fprintf(&b, "Dungeon sweep: %d items → %s", len(plans), status)
	} else {
		fmt.Fprintf(&b, "Dungeon sweep: %d items", len(plans))
	}
	for _, pi := range plans {
		fmt.Fprintf(&b, "\n  - %s → %s", pi.preview.Item, pi.preview.DestRel)
	}
	return b.String()
}

func aggregateMoveErrors(failures []error) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%d item(s) failed validation; no moves were applied:", len(failures))
	for _, err := range failures {
		fmt.Fprintf(&b, "\n  - %v", err)
	}
	return camperrors.New(b.String())
}

func previewsOf(plans []*plannedItem) []movePreview {
	out := make([]movePreview, 0, len(plans))
	for _, pi := range plans {
		out = append(out, pi.preview)
	}
	return out
}

func renderDryRun(w io.Writer, previews []movePreview, asJSON bool) error {
	if asJSON {
		return renderDryRunJSON(w, previews)
	}
	return renderDryRunText(w, previews)
}

func renderDryRunText(w io.Writer, previews []movePreview) error {
	var b strings.Builder
	fmt.Fprintln(&b, "Dry run: no filesystem changes, no commit.")
	fmt.Fprintln(&b)
	for _, p := range previews {
		label := p.Status
		if label == "" {
			label = "dungeon root"
		}
		fmt.Fprintf(&b, "  %s  %s → %s  [%s]\n", p.Item, p.SourceRel, p.DestRel, label)
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Would move %d item(s); a commit would be created.\n", len(previews))
	_, err := io.WriteString(w, b.String())
	return err
}

func renderDryRunJSON(w io.Writer, previews []movePreview) error {
	payload := struct {
		SchemaVersion string        `json:"schema_version"`
		DryRun        bool          `json:"dry_run"`
		WouldCommit   bool          `json:"would_commit"`
		Count         int           `json:"count"`
		Moves         []movePreview `json:"moves"`
	}{
		SchemaVersion: "1",
		DryRun:        true,
		WouldCommit:   len(previews) > 0,
		Count:         len(previews),
		Moves:         previews,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal dry-run plan")
	}
	_, err = io.WriteString(w, string(data)+"\n")
	return err
}

func runWorkitemMove(ctx context.Context, cmd *cobra.Command, target, status string, f moveFlags) error {
	if f.dryRun {
		preview, err := planWorkitemMove(ctx, target, status)
		if err != nil {
			return err
		}
		return renderDryRun(os.Stdout, []movePreview{preview}, f.jsonOut)
	}
	move, err := moveWorkitemToDungeon(ctx, cmd, target, status)
	if err != nil {
		return err
	}
	return CommitDungeonMove(ctx, move)
}

func planWorkitemMove(ctx context.Context, target, status string) (movePreview, error) {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return movePreview{}, camperrors.Wrap(err, "not in a campaign directory")
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	items, err := wkitem.Discover(ctx, campaignRoot, resolver)
	if err != nil {
		return movePreview{}, camperrors.Wrap(err, "discovering work items")
	}

	item, err := selectWorkitemDungeonTarget(campaignRoot, items, target)
	if err != nil {
		return movePreview{}, err
	}
	resolved, err := resolveWorkitemDungeonTarget(campaignRoot, item)
	if err != nil {
		return movePreview{}, err
	}

	destAbs := filepath.Join(resolved.DungeonPath, resolved.ItemName)
	kind := intdungeon.MoveKindTriageRoot
	if status != "" {
		svc := intdungeon.NewService(campaignRoot, resolved.DungeonPath)
		mp, err := svc.PlanMoveToDungeonStatus(ctx, resolved.ItemName, resolved.ParentPath, status)
		if err != nil {
			return movePreview{}, WrapDungeonMoveError(err, resolved.ItemName, status)
		}
		destAbs = mp.Destination
		kind = intdungeon.MoveKindTriageStatus
	}

	return movePreview{
		Item:      resolved.ItemName,
		SourceRel: RelFromRoot(campaignRoot, resolved.SourcePath),
		DestRel:   RelFromRoot(campaignRoot, destAbs),
		Status:    status,
		Mode:      string(kind),
	}, nil
}
