package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/locate"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// resolveSweepLocation resolves the locate.Location for a sweep candidate from
// its RelativePath, reusing locate.DetectFromCwd (which is generic over any
// path under the item, not literally the process cwd) so the sweep and
// interactive promote share one notion of "where this item's dungeon lives."
// Every candidate PlanSweep can produce today has a workflow/<type>/<slug>
// RelativePath, so this is currently a pass-through onto DetectFromCwd's
// existing resolution; phase 3 (rail residents in festivals/) extends
// DetectFromCwd itself, and this function needs no change when that lands.
func resolveSweepLocation(campaignRoot string, item wkitem.WorkItem) (*locate.Location, error) {
	return locate.DetectFromCwd(campaignRoot, filepath.Join(campaignRoot, item.RelativePath))
}

type sweepOptions struct {
	DryRun bool
	JSON   bool
}

// workitemSweepResult is the --json envelope for camp workitem sweep. It follows
// the batch-result skeleton established by workitemGatherResult (schema version,
// generated-at, dry-run flag, per-item slice, top-level committed/warnings).
type workitemSweepResult struct {
	SchemaVersion string                    `json:"schema_version"`
	GeneratedAt   time.Time                 `json:"generated_at"`
	DryRun        bool                      `json:"dry_run,omitempty"`
	Candidates    int                       `json:"candidates"`
	Swept         int                       `json:"swept"`
	Failed        int                       `json:"failed"`
	Items         []workitemSweepResultItem `json:"items"`
	Committed     bool                      `json:"committed"`
	Warnings      []string                  `json:"warnings,omitempty"`
}

type workitemSweepResultItem struct {
	ID        string `json:"id,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Type      string `json:"type"`
	From      string `json:"from"`
	To        string `json:"to,omitempty"`
	Evidence  string `json:"evidence,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Committed bool   `json:"committed"`
	Error     string `json:"error,omitempty"`
}

func newSweepCommand() *cobra.Command {
	var (
		dryRun  bool
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Promote workitems with completed runs",
		Long: `Promote every workitem whose active workflow run has completed to its
local dungeon (tier-1 evidence-driven completion).

Only loop-completion evidence (workflow_run_completed) drives this sweep, and it
only ever auto-promotes; merged-branch evidence is handled separately by camp
fresh, which prompts. Festivals and intents are excluded.

Each eligible item moves independently: a failure on one (dirty git state, a
path collision at its destination) is reported and the sweep continues to the
next. Use --dry-run to see the plan without moving anything, and --json for a
structured result. In table mode any per-item failure yields a non-zero exit,
matching camp fresh; --json reports failures in the payload (failed count and
per-item error) and stays exit 0 so the structured result is the contract.`,
		Args: jsoncontract.Args(WorkitemSweepJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Fully specified by flags; no interactive selection",
		},
		RunE: jsoncontract.RunE(WorkitemSweepJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, _ []string) error {
			return runWorkitemSweep(cmd, sweepOptions{DryRun: dryRun, JSON: jsonOut})
		}),
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "Print the sweep plan, change nothing")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	return cmd
}

// gatherSweepCandidates loads the campaign and returns the eligible tier-1
// candidates from one discovery pass. Shared by the sweep command and camp
// fresh's completed_runs handling so both plan against the same read.
func gatherSweepCandidates(ctx context.Context) (*config.CampaignConfig, string, []wkitem.SweepCandidate, error) {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, "", nil, camperrors.Wrap(err, "not in a campaign directory")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, "", nil, camperrors.Wrap(err, "discovering workitems")
	}
	return cfg, root, wkitem.PlanSweep(items), nil
}

func newSweepResult(dryRun bool, candidates int) workitemSweepResult {
	return workitemSweepResult{
		SchemaVersion: WorkitemSweepJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		DryRun:        dryRun,
		Candidates:    candidates,
	}
}

// executeSweepCandidates runs the per-item promote loop with error isolation,
// filling result. Shared by the sweep command and camp fresh.
func executeSweepCandidates(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, candidates []wkitem.SweepCandidate, result *workitemSweepResult) error {
	for _, cand := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := sweepOne(ctx, cmd, cfg, root, cand, result)
		result.Items = append(result.Items, item)
		if item.Error != "" {
			result.Failed++
		} else {
			result.Swept++
		}
	}
	result.Committed = result.Failed == 0 && result.Swept > 0
	return nil
}

func runWorkitemSweep(cmd *cobra.Command, opts sweepOptions) error {
	ctx := cmd.Context()
	cfg, root, candidates, err := gatherSweepCandidates(ctx)
	if err != nil {
		return err
	}
	result := newSweepResult(opts.DryRun, len(candidates))

	if opts.DryRun {
		fillSweepPlan(root, candidates, &result)
		return emitSweepResult(cmd, &result, opts.JSON)
	}

	// A mid-sweep context cancellation must propagate, never report as a clean
	// success; emit whatever completed before surfacing the cancellation.
	sweepErr := executeSweepCandidates(ctx, cmd, cfg, root, candidates, &result)

	if err := emitSweepResult(cmd, &result, opts.JSON); err != nil {
		return err
	}
	if sweepErr != nil {
		return sweepErr
	}
	// Table mode follows camp fresh: any per-item failure is a non-zero exit.
	// JSON mode follows camp workitem commits: per-item failures live in the
	// payload (failed count and per-item error), and the command exits 0 so the
	// structured result stays the single, clean contract on stdout.
	if result.Failed > 0 && !opts.JSON {
		return camperrors.Newf("%d workitem(s) failed to sweep", result.Failed)
	}
	return nil
}

// sweepOne executes a single candidate's promotion with the same move, audit,
// ledger, link-release, commit, and nav-invalidation sequence doDungeonPromote
// uses for one item, isolated so a failure returns a populated result entry
// instead of aborting the batch. The move either fully applies (move + audit +
// ledger + link release + commit) or the entry carries an error; nothing is
// reported swept without the move landing.
func sweepOne(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, cand wkitem.SweepCandidate, result *workitemSweepResult) workitemSweepResultItem {
	entry := workitemSweepResultItem{
		Type:     string(cand.Item.WorkflowType),
		Evidence: cand.Reason,
		RunID:    cand.RunID,
	}

	loc, err := resolveSweepLocation(root, cand.Item)
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	entry.From = filepath.ToSlash(dungeoncmd.RelFromRoot(root, loc.SourcePath))

	// Read the marker before the move: MoveToDungeon relocates loc.SourcePath,
	// so every identity below must be captured now.
	ident := sweptWorkitemIdentity(ctx, loc, cand.Item)
	entry.ID, entry.Ref = ident.LedgerID, ident.LedgerRef

	// Capture link identity before the move: workitem_key encodes the source
	// path, which is about to change (same reason doDungeonPromote captures it).
	oldKey := cand.Item.Key
	if oldKey == "" {
		oldKey = entry.Type + ":" + entry.From
	}

	moveRes, err := MoveToDungeon(ctx, root, loc, "completed")
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	entry.To = moveRes.ToRel

	// Shelving ends the workitem's active life: drop its links so a multi-
	// worktree design does not leave stale rows that only resolve to a dungeon
	// path the selector cannot see. Matches doDungeonPromote.
	dropped, unlinkErr := unlinkShelvedWorkitem(ctx, root, ident.LinkID, oldKey)
	if unlinkErr != nil {
		entry.Error = unlinkErr.Error()
		return entry
	}

	appendWorkitemAuditEvent(ctx, cmd, root, wkaudit.Event{
		Event:    wkaudit.EventPromote,
		ID:       ident.LedgerID,
		Ref:      ident.LedgerRef,
		Title:    ident.LedgerTitle,
		Type:     entry.Type,
		From:     entry.From,
		To:       entry.To,
		Target:   "completed",
		Evidence: cand.Reason,
	})

	ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindTransitioned, ledgerkit.Scope{Workitem: ident.LedgerID},
			ledger.WithWhy("sweep promote to completed"),
			ledger.WithPayload(map[string]any{
				"target": "completed", "from": entry.From, "to": entry.To,
				"evidence": cand.Reason, "run_id": cand.RunID,
			}))

	destPaths := append([]string{moveRes.TargetPath}, moveRes.CreatedFiles...)
	destPaths = append(destPaths, filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile))
	if len(dropped) > 0 {
		destPaths = append(destPaths, links.LinksPath(root))
		for _, l := range dropped {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  released link %s (%s:%s); %s is no longer active\n",
				l.ID, l.ScopeKind, l.ScopePath, ident.LedgerID)
		}
	}
	outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
		Config:           cfg,
		CampaignRoot:     root,
		Description:      fmt.Sprintf("Promote workitem %s to completed (sweep: %s)", loc.Slug, cand.Reason),
		SourcePaths:      []string{loc.SourcePath},
		DestinationPaths: destPaths,
		RewrittenFiles:   moveRes.Svc.RewrittenLinkFiles(),
	})
	entry.Committed = outcome.Committed
	if cerr := outcome.Err(); cerr != nil {
		entry.Error = cerr.Error()
		return entry
	}

	if navErr := navindex.Delete(root); navErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to invalidate navigation cache after %s: %v", ident.LedgerID, navErr))
	}
	return entry
}

// sweptIdentity carries the identities sweepOne captures from one pre-move
// marker read. LedgerID/Ref/Title are a display identity for the audit trail,
// the ledger, and the result envelope. LinkID is the stable workitem_id the link
// registry keys on. They are deliberately separate: see sweptWorkitemIdentity.
type sweptIdentity struct {
	LinkID      string
	LedgerID    string
	LedgerRef   string
	LedgerTitle string
}

// sweptWorkitemIdentity reads the .workitem marker at loc.SourcePath before the
// move and splits what it finds into a display identity and a link identity.
//
// LedgerID falls back to the slug when no marker resolves, so a directory that
// was never adopted still sweeps instead of failing the command (matching
// runWorkitemPromote's ledger capture).
//
// LinkID must never carry that fallback. unlinkShelvedWorkitem matches on
// workitem_id OR workitem_key, so a slug in the ID position would drop the links
// of any unrelated workitem whose real workitem_id happened to equal this slug,
// and link deletion is the destructive half of a sweep. An unmarked source
// therefore gets an empty LinkID and is matched by key alone -- the same
// separation doDungeonPromote gets from promotedWorkitemIdentity, which returns
// an empty id for an unmarked directory.
//
// item.StableID is the fallback because it is the marker id resolved at
// discovery (empty when unmarked, never a slug), which covers a file-shaped
// source whose identity lives in its own frontmatter rather than in a .workitem
// sibling LoadMetadata can read.
func sweptWorkitemIdentity(ctx context.Context, loc *locate.Location, item wkitem.WorkItem) sweptIdentity {
	ident := sweptIdentity{LedgerID: loc.Slug}
	if meta, err := wkitem.LoadMetadata(ctx, loc.SourcePath); err == nil && meta != nil {
		ident.LinkID = meta.ID
		ident.LedgerID, ident.LedgerRef, ident.LedgerTitle = meta.ID, meta.Ref, meta.Title
	}
	if ident.LinkID == "" {
		ident.LinkID = item.StableID
	}
	return ident
}

// fillSweepPlan populates result.Items with the dry-run plan: what each
// candidate would move where, mutating nothing. The caller emits result.
func fillSweepPlan(root string, candidates []wkitem.SweepCandidate, result *workitemSweepResult) {
	for _, cand := range candidates {
		entry := workitemSweepResultItem{
			ID:       cand.Item.Key,
			Type:     string(cand.Item.WorkflowType),
			From:     filepath.ToSlash(cand.Item.RelativePath),
			Evidence: cand.Reason,
			RunID:    cand.RunID,
		}
		if loc, err := resolveSweepLocation(root, cand.Item); err == nil {
			entry.To = filepath.ToSlash(dungeoncmd.RelFromRoot(root, filepath.Join(loc.DungeonPath, "completed")))
		} else {
			entry.Error = err.Error()
		}
		result.Items = append(result.Items, entry)
	}
}

// Fresh sweep modes for camp fresh's completed_runs setting.
const (
	FreshSweepModeReport = "report"
	FreshSweepModeSweep  = "sweep"
)

// RunFreshSweep runs the tier-1 workitem sweep for camp fresh, reusing the same
// internals as camp workitem sweep. mode "report" prints the read-only banner;
// mode "sweep" executes the promotion. Callers must not pass "off" (an opted-out
// campaign must pay for no discovery pass, which the caller guards). When nothing
// is eligible, RunFreshSweep is silent and mutates nothing.
func RunFreshSweep(ctx context.Context, out io.Writer, mode string) error {
	cfg, root, candidates, err := gatherSweepCandidates(ctx)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	if mode == FreshSweepModeReport {
		_, err := fmt.Fprintln(out, wkitem.SweepBannerText(len(candidates)))
		return err
	}

	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(out)
	cmd.SetErr(out)
	result := newSweepResult(false, len(candidates))
	sweepErr := executeSweepCandidates(ctx, cmd, cfg, root, candidates, &result)
	if err := emitSweepResult(cmd, &result, false); err != nil {
		return err
	}
	if sweepErr != nil {
		return sweepErr
	}
	if result.Failed > 0 {
		return camperrors.Newf("%d workitem(s) failed to sweep", result.Failed)
	}
	return nil
}

// PromoteMergedWorkitem promotes wi to its local dungeon/completed with the
// given evidence, reusing the sweep's per-item move + audit + ledger + commit
// path. Used by camp fresh's tier-2 merged-branch backstop when a human accepts
// the prompt; evidence is EvidenceMergedBranch. Never called on inference
// evidence without an explicit human accept upstream. Takes the already-resolved
// cfg and root from the caller so a non-root cwd cannot resolve a different
// campaign than the one fresh is operating on.
func PromoteMergedWorkitem(ctx context.Context, out io.Writer, cfg *config.CampaignConfig, root string, wi wkitem.WorkItem, evidence string) error {
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(out)
	cmd.SetErr(out)
	var result workitemSweepResult
	item := sweepOne(ctx, cmd, cfg, root, wkitem.SweepCandidate{Item: wi, Reason: evidence}, &result)
	if item.Error != "" {
		return camperrors.New(item.Error)
	}
	return nil
}

func emitSweepResult(cmd *cobra.Command, result *workitemSweepResult, jsonOut bool) error {
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return camperrors.Wrap(err, "encoding JSON output")
		}
		return nil
	}

	out := cmd.OutOrStdout()
	if result.DryRun {
		if len(result.Items) == 0 {
			_, err := fmt.Fprintln(out, "No workitems with completed runs to sweep.")
			return err
		}
		if _, err := fmt.Fprintf(out, "Sweep plan (%d workitem(s) with completed runs):\n", len(result.Items)); err != nil {
			return err
		}
		for _, it := range result.Items {
			if _, err := fmt.Fprintf(out, "  %s (%s) -> %s\n", it.From, it.Type, it.To); err != nil {
				return err
			}
		}
		return nil
	}

	if result.Candidates == 0 {
		_, err := fmt.Fprintln(out, "No workitems with completed runs to sweep.")
		return err
	}
	for _, it := range result.Items {
		line := fmt.Sprintf("  %s %s -> %s\n", ui.SuccessIcon(), it.From, it.To)
		if it.Error != "" {
			line = fmt.Sprintf("  %s %s (%s): %s\n", ui.WarningIcon(), it.From, it.Type, it.Error)
		}
		if _, err := fmt.Fprint(out, line); err != nil {
			return err
		}
	}
	summary := fmt.Sprintf("%s Swept %d workitem(s) to completed\n", ui.SuccessIcon(), result.Swept)
	if result.Failed > 0 {
		summary = fmt.Sprintf("%s %d swept, %d failed\n", ui.WarningIcon(), result.Swept, result.Failed)
	}
	_, err := fmt.Fprint(out, summary)
	return err
}
