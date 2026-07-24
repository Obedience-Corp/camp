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
	ID          string `json:"id,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Type        string `json:"type"`
	From        string `json:"from"`
	To          string `json:"to,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	ActiveRunID string `json:"active_run_id,omitempty"`
	Committed   bool   `json:"committed"`
	Error       string `json:"error,omitempty"`
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
func executeSweepCandidates(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, candidates []wkitem.SweepCandidate, result *workitemSweepResult) {
	for _, cand := range candidates {
		if ctx.Err() != nil {
			return
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

	executeSweepCandidates(ctx, cmd, cfg, root, candidates, &result)

	if err := emitSweepResult(cmd, &result, opts.JSON); err != nil {
		return err
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
// ledger, commit, and nav-invalidation sequence runWorkitemPromote uses for one
// item, isolated so a failure returns a populated result entry instead of
// aborting the batch. The move either fully applies (move + audit + ledger +
// commit) or the entry carries an error; nothing is reported swept without the
// move landing.
func sweepOne(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, cand wkitem.SweepCandidate, result *workitemSweepResult) workitemSweepResultItem {
	entry := workitemSweepResultItem{
		Type:        string(cand.Item.WorkflowType),
		Evidence:    cand.Reason,
		ActiveRunID: cand.ActiveRunID,
	}

	loc, err := resolveSweepLocation(root, cand.Item)
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	entry.From = filepath.ToSlash(dungeoncmd.RelFromRoot(root, loc.SourcePath))

	// Read the marker before the move: MoveToDungeon relocates loc.SourcePath,
	// so the id/ref/title used for ledger correlation must be captured now. A
	// missing marker falls back to the slug, matching runWorkitemPromote.
	ledgerID, ledgerRef, ledgerTitle := loc.Slug, "", ""
	if meta, metaErr := wkitem.LoadMetadata(ctx, loc.SourcePath); metaErr == nil && meta != nil {
		ledgerID, ledgerRef, ledgerTitle = meta.ID, meta.Ref, meta.Title
	}
	entry.ID, entry.Ref = ledgerID, ledgerRef

	moveRes, err := MoveToDungeon(ctx, root, loc, "completed")
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	entry.To = moveRes.ToRel

	appendWorkitemAuditEvent(ctx, cmd, root, wkaudit.Event{
		Event:    wkaudit.EventPromote,
		ID:       ledgerID,
		Ref:      ledgerRef,
		Title:    ledgerTitle,
		Type:     entry.Type,
		From:     entry.From,
		To:       entry.To,
		Target:   "completed",
		Evidence: cand.Reason,
	})

	ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindTransitioned, ledgerkit.Scope{Workitem: ledgerID},
			ledger.WithWhy("sweep promote to completed"),
			ledger.WithPayload(map[string]any{
				"target": "completed", "from": entry.From, "to": entry.To,
				"evidence": cand.Reason, "active_run_id": cand.ActiveRunID,
			}))

	destPaths := append([]string{moveRes.TargetPath}, moveRes.CreatedFiles...)
	destPaths = append(destPaths, filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile))
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
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to invalidate navigation cache after %s: %v", ledgerID, navErr))
	}
	return entry
}

// fillSweepPlan populates result.Items with the dry-run plan: what each
// candidate would move where, mutating nothing. The caller emits result.
func fillSweepPlan(root string, candidates []wkitem.SweepCandidate, result *workitemSweepResult) {
	for _, cand := range candidates {
		entry := workitemSweepResultItem{
			ID:          cand.Item.Key,
			Type:        string(cand.Item.WorkflowType),
			From:        filepath.ToSlash(cand.Item.RelativePath),
			Evidence:    cand.Reason,
			ActiveRunID: cand.ActiveRunID,
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
	executeSweepCandidates(ctx, cmd, cfg, root, candidates, &result)
	if err := emitSweepResult(cmd, &result, false); err != nil {
		return err
	}
	if result.Failed > 0 {
		return camperrors.Newf("%d workitem(s) failed to sweep", result.Failed)
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
