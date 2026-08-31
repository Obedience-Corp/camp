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
	"github.com/Obedience-Corp/camp/internal/triage"
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
// It stays a pass-through: rail residents (festivals/<stage>/<slug>) resolve
// because DetectFromCwd itself learned that layout, so a swept resident lands in
// the festival-local dungeon without this function knowing the difference.
func resolveSweepLocation(campaignRoot string, item wkitem.WorkItem) (*locate.Location, error) {
	return locate.DetectFromCwd(campaignRoot, filepath.Join(campaignRoot, item.RelativePath))
}

type sweepOptions struct {
	DryRun bool
	JSON   bool
	Prompt bool
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
	// Skipped names every workitem the sweep looked at and deliberately did not
	// move, with the reason. Camp acting on its own is only acceptable while it
	// reports what it declined to do as plainly as what it did.
	Skipped []workitemSweepSkip `json:"skipped,omitempty"`
	// ReleasedLinks names every link dropped because the workitem holding it was
	// shelved, in enough detail to put it back by hand.
	ReleasedLinks []releasedLink `json:"released_links,omitempty"`
	Committed     bool           `json:"committed"`
	Warnings      []string       `json:"warnings,omitempty"`
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

// workitemSweepSkip is one reported non-move: what was looked at, and why it
// stayed put. Reason is a stable code (wkitem.Skip*), Detail the sentence.
type workitemSweepSkip struct {
	ID     string `json:"id,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Type   string `json:"type"`
	From   string `json:"from"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
	RunID  string `json:"run_id,omitempty"`
}

func newSweepCommand() *cobra.Command {
	var (
		dryRun  bool
		jsonOut bool
		prompt  bool
	)
	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Act on workitems with completed runs",
		Long: `Act on every workitem whose workflow run has completed (tier-1
evidence-driven completion).

What a completed run entitles a workitem to depends on its type:

  bug, chore, feature, custom   The loop was the work, so the item is promoted
                                to its local dungeon/completed.
  explore, research             The loop produced findings that need a home.
  design                        Never promoted here: a design is done when it is
                                implemented, so it waits for a merged branch or a
                                completed festival instead.

Without --prompt the command promotes what it can and reports the rest, which is
what an agent or a script wants. With --prompt it asks per item on a terminal and
can also route findings into docs/, shelve, or leave the item alone; on a non-TTY
or with --json it reports instead, because nothing can answer.

Two guards apply in every mode. A directory written within the last ten minutes
is left alone (a session is probably still working in it), and without --prompt a
directory holding a link is left alone too rather than moved out from under
whoever holds it. Both are reported with their reason.

Only loop-completion evidence (workflow_run_completed) drives this sweep;
merged-branch evidence is handled separately by camp fresh. Festivals and intents
are excluded.

Each item moves independently: a failure on one (dirty git state, a path
collision at its destination) is reported and the sweep continues to the next.
Use --dry-run to see the plan without moving anything, and --json for a
structured result. In table mode any per-item failure yields a non-zero exit,
matching camp fresh; --json reports failures in the payload (failed count and
per-item error) and stays exit 0 so the structured result is the contract.`,
		Args: jsoncontract.Args(WorkitemSweepJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Fully specified by flags; --prompt reports instead of asking when there is no terminal",
		},
		RunE: jsoncontract.RunE(WorkitemSweepJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, _ []string) error {
			return runWorkitemSweep(cmd, sweepOptions{DryRun: dryRun, JSON: jsonOut, Prompt: prompt})
		}),
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "Print the sweep plan, change nothing")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	f.BoolVar(&prompt, "prompt", false, "Ask per workitem on a terminal instead of promoting automatically")
	return cmd
}

// gatherSweepPlan loads the campaign and classifies every workitem with a
// completed run from one discovery pass. Shared by the sweep command and camp
// fresh's completed_runs handling so both plan against the same read.
func gatherSweepPlan(ctx context.Context) (*config.CampaignConfig, string, wkitem.SweepPlan, error) {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, "", wkitem.SweepPlan{}, camperrors.Wrap(err, "not in a camp directory")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, "", wkitem.SweepPlan{}, camperrors.Wrap(err, "discovering workitems")
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

// sweepWork is one resolved sweep: the campaign, the mode, what may be acted on
// after the guards ran, and everything deliberately left alone with its reason.
// LinkedScopes carries the links found inside each actionable candidate, keyed by
// campaign-relative path, so the prompt can name them without a second registry
// read.
type sweepWork struct {
	cfg          *config.CampaignConfig
	root         string
	actionable   []wkitem.SweepCandidate
	skipped      []wkitem.SweepSkip
	linkedScopes map[string][]links.Link
}

// resolveSweepWork plans against one discovery pass and applies the guards for
// mode. The clock is read once and the link registry loaded once, so every
// candidate in a run is judged against the same snapshot rather than a moving
// one.
func resolveSweepWork(ctx context.Context, mode string) (*sweepWork, error) {
	cfg, root, plan, err := gatherSweepPlan(ctx)
	if err != nil {
		return nil, err
	}
	work := &sweepWork{
		cfg:          cfg,
		root:         root,
		skipped:      plan.Skipped,
		linkedScopes: map[string][]links.Link{},
	}
	if len(plan.Candidates) == 0 {
		return work, nil
	}

	registry, err := links.Load(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "loading workitem link registry")
	}
	now := time.Now()
	auto := mode == FreshSweepModeSweep

	for _, cand := range plan.Candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Automatic mode has no answer for where findings should go, so it says
		// so instead of guessing. The prompt is where that question belongs.
		if auto && cand.Disposition == wkitem.DispositionRoute {
			work.skipped = append(work.skipped, wkitem.SweepSkip{
				Item:   cand.Item,
				Reason: wkitem.SkipNeedsRouting,
				Detail: "findings need a destination; run camp workitem sweep --prompt to choose one",
				RunID:  cand.RunID,
			})
			continue
		}

		scoped := linksWithin(registry, cand.Item.RelativePath)
		if auto && len(scoped) > 0 {
			work.skipped = append(work.skipped, wkitem.SweepSkip{
				Item:   cand.Item,
				Reason: wkitem.SkipLinkedScope,
				Detail: linkedScopeDetail(scoped) + "; run camp workitem sweep --prompt to review and release them",
				RunID:  cand.RunID,
			})
			continue
		}

		newest, mtErr := wkitem.NewestContentModTime(ctx, cand.Item.AbsPath(root))
		if mtErr != nil {
			if ctx.Err() != nil {
				return nil, mtErr
			}
			// An unreadable directory is a real failure the per-item loop should
			// report, not a reason to silently drop the candidate.
			work.actionable = append(work.actionable, cand)
			continue
		}
		if wkitem.IsFreshlyWritten(newest, now) {
			// The override is named in the same line as the refusal. A guard
			// that acts on the user's behalf owes them the one command that
			// overrules it, or it stops being a guard and becomes a wall.
			work.skipped = append(work.skipped, wkitem.SweepSkip{
				Item:   cand.Item,
				Reason: wkitem.SkipRecentWrites,
				Detail: wkitem.FreshWriteDetail(newest, now) + "; run " + promoteCommandFor(cand.Item) + " to move it anyway",
				RunID:  cand.RunID,
			})
			continue
		}

		if len(scoped) > 0 {
			work.linkedScopes[cand.Item.RelativePath] = scoped
		}
		work.actionable = append(work.actionable, cand)
	}
	return work, nil
}

// linksWithin returns the links whose scope is the workitem directory itself or
// something nested under it. These are the rows a move orphans, which is why the
// automatic path refuses to move such a directory and the prompt names them
// before asking.
func linksWithin(registry *links.Links, relPath string) []links.Link {
	if registry == nil {
		return nil
	}
	var out []links.Link
	for _, l := range registry.Links {
		if links.ScopeWithin(l.Scope.Path, relPath) {
			out = append(out, l)
		}
	}
	return out
}

// promoteCommandFor renders the copy-pasteable command that moves a workitem the
// guards declined to move, addressed by the id the selector resolves.
func promoteCommandFor(item wkitem.WorkItem) string {
	return "camp workitem promote " + wkitem.LinkWorkitemID(&item) + " --target completed"
}

func linkedScopeDetail(scoped []links.Link) string {
	if len(scoped) == 1 {
		return fmt.Sprintf("link %s is scoped inside it (%s:%s); a session may still hold it",
			scoped[0].ID, scoped[0].Scope.Kind, scoped[0].Scope.Path)
	}
	return fmt.Sprintf("%d links are scoped inside it; a session may still hold them", len(scoped))
}

// fillSweepSkips copies the planner and guard skips onto the result envelope.
func fillSweepSkips(work *sweepWork, result *workitemSweepResult) {
	for _, s := range work.skipped {
		result.Skipped = append(result.Skipped, workitemSweepSkip{
			ID:     wkitem.LinkWorkitemID(&s.Item),
			Ref:    refOfItem(s.Item),
			Type:   string(s.Item.WorkflowType),
			From:   filepath.ToSlash(s.Item.RelativePath),
			Reason: s.Reason,
			Detail: s.Detail,
			RunID:  s.RunID,
		})
	}
}

func refOfItem(item wkitem.WorkItem) string {
	if item.SourceMetadata == nil {
		return ""
	}
	ref, _ := item.SourceMetadata["ref"].(string)
	return ref
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

	configured := FreshSweepModeSweep
	if opts.Prompt {
		configured = FreshSweepModePrompt
	}
	mode := resolveSweepMode(configured, ui.IsTerminal(), opts.JSON, opts.DryRun)

	work, err := resolveSweepWork(ctx, mode)
	if err != nil {
		return err
	}
	result := newSweepResult(opts.DryRun, len(work.actionable))
	fillSweepSkips(work, &result)

	if mode == FreshSweepModeReport {
		if opts.JSON {
			fillSweepPlan(work.root, work.actionable, &result)
			return emitSweepResult(cmd, &result, true)
		}
		if opts.DryRun {
			fillSweepPlan(work.root, work.actionable, &result)
			return emitSweepResultWithNotice(ctx, cmd, work, &result)
		}
		return emitSweepReport(ctx, cmd.OutOrStdout(), work)
	}

	if mode == FreshSweepModePrompt {
		return runSweepPrompt(ctx, cmd, work)
	}

	// A mid-sweep context cancellation must propagate, never report as a clean
	// success; emit whatever completed before surfacing the cancellation.
	sweepErr := executeSweepCandidates(ctx, cmd, work.cfg, work.root, work.actionable, &result)

	if opts.JSON {
		if err := emitSweepResult(cmd, &result, true); err != nil {
			return err
		}
	} else if err := emitSweepResultWithNotice(ctx, cmd, work, &result); err != nil {
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
	return sweepOneToStatus(ctx, cmd, cfg, root, cand, "completed", result)
}

// sweepOneToStatus is sweepOne with the dungeon status chosen by the caller, so
// the prompt's "someday" and "archive" answers reuse the identical move, audit,
// ledger, link-release, and commit sequence rather than a second implementation
// that could drift from it.
func sweepOneToStatus(ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig, root string, cand wkitem.SweepCandidate, status string, result *workitemSweepResult) workitemSweepResultItem {
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

	moveRes, err := MoveToDungeon(ctx, root, loc, status)
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	entry.To = moveRes.ToRel

	// Shelving ends the workitem's active life: drop its links so a multi-
	// worktree design does not leave stale rows that only resolve to a dungeon
	// path the selector cannot see. Matches doDungeonPromote, including the
	// path arm that catches links made under a directory's earlier name.
	dropped, unlinkErr := unlinkShelvedWorkitem(ctx, root, ident.LinkID, oldKey, entry.From)
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
		Target:   status,
		Evidence: cand.Reason,
	})

	ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindTransitioned, ledgerkit.Scope{Workitem: ident.LedgerID},
			ledger.WithWhy("sweep promote to "+status),
			ledger.WithPayload(map[string]any{
				"target": status, "from": entry.From, "to": entry.To,
				"evidence": cand.Reason, "run_id": cand.RunID,
			}))

	destPaths := append([]string{moveRes.TargetPath}, moveRes.CreatedFiles...)
	destPaths = append(destPaths, filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile))
	if len(dropped) > 0 {
		destPaths = append(destPaths, links.LinksPath(root))
		for i := range dropped {
			dropped[i].Workitem = ident.LedgerID
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  released link %s (%s:%s); %s is no longer active\n",
				dropped[i].ID, dropped[i].ScopeKind, dropped[i].ScopePath, ident.LedgerID)
		}
		if result != nil {
			result.ReleasedLinks = append(result.ReleasedLinks, dropped...)
		}
	}
	outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
		Config:           cfg,
		CampaignRoot:     root,
		Description:      fmt.Sprintf("Promote workitem %s to %s (sweep: %s)", loc.Slug, status, cand.Reason),
		SourcePaths:      []string{loc.SourcePath},
		DestinationPaths: destPaths,
		RewrittenFiles:   moveRes.Svc.RewrittenLinkFiles(),
	})
	entry.Committed = outcome.Committed
	if cerr := outcome.Err(); cerr != nil {
		entry.Error = cerr.Error()
		return entry
	}

	if navErr := navindex.Delete(root); navErr != nil && result != nil {
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

// sweptWorkitemIdentity splits the .workitem marker into a display identity
// and a link identity. LinkID must never carry the slug fallback that LedgerID
// uses: the unlink matcher compares workitem ids, and a slug there would drop
// an unrelated workitem's links. Unmarked sources match by key and path only.
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
//
// A route candidate gets no destination, because there is not one to predict:
// where explore or research findings belong is the question the prompt asks, and
// naming dungeon/completed here would claim an answer nobody gave.
func fillSweepPlan(root string, candidates []wkitem.SweepCandidate, result *workitemSweepResult) {
	for _, cand := range candidates {
		entry := workitemSweepResultItem{
			ID:       cand.Item.Key,
			Type:     string(cand.Item.WorkflowType),
			From:     filepath.ToSlash(cand.Item.RelativePath),
			Evidence: cand.Reason,
			RunID:    cand.RunID,
		}
		if cand.Disposition == wkitem.DispositionRoute {
			result.Items = append(result.Items, entry)
			continue
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
	FreshSweepModeOff    = "off"
	FreshSweepModeReport = "report"
	FreshSweepModeSweep  = "sweep"
	FreshSweepModePrompt = "prompt"
)

// resolveSweepMode is the single place the flag and the fresh setting resolve.
// Invariants: dry-run is always report; prompt with no terminal or with --json
// is report (nobody can answer); unrecognized falls back to sweep, which only an
// explicit caller reaches.
func resolveSweepMode(configured string, terminal, jsonOut, dryRun bool) string {
	if configured == FreshSweepModeOff {
		return FreshSweepModeOff
	}
	if dryRun {
		return FreshSweepModeReport
	}
	switch configured {
	case FreshSweepModeReport:
		return FreshSweepModeReport
	case FreshSweepModePrompt:
		if !terminal || jsonOut {
			return FreshSweepModeReport
		}
		return FreshSweepModePrompt
	default:
		return FreshSweepModeSweep
	}
}

// RunFreshSweep runs the tier-1 workitem sweep for camp fresh, reusing the same
// internals as camp workitem sweep. mode "prompt" asks per workitem on a
// terminal and reports otherwise; "report" prints the read-only banner and the
// per-item reasons; "sweep" promotes what it can automatically. Callers must not
// pass "off" (an opted-out campaign must pay for no discovery pass, which the
// caller guards). When nothing is eligible and nothing was skipped, RunFreshSweep
// is silent and mutates nothing.
func RunFreshSweep(ctx context.Context, out io.Writer, mode string) error {
	mode = resolveSweepMode(mode, ui.IsTerminal(), false, false)
	if mode == FreshSweepModeOff {
		// The caller is supposed to have skipped the call entirely; returning
		// here as well means a missed guard costs a no-op rather than a
		// discovery pass an opted-out campaign never asked to pay for.
		return nil
	}

	work, err := resolveSweepWork(ctx, mode)
	if err != nil {
		return err
	}
	if len(work.actionable) == 0 && len(work.skipped) == 0 {
		// Nothing to sweep, but camp fresh is still a high-traffic surface:
		// print the stale-triage notice when there is one.
		return triage.WriteBanner(ctx, out, work.root, time.Now())
	}

	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(out)
	cmd.SetErr(out)

	switch mode {
	case FreshSweepModeReport:
		return emitSweepReport(ctx, out, work)
	case FreshSweepModePrompt:
		return runSweepPrompt(ctx, cmd, work)
	}

	result := newSweepResult(false, len(work.actionable))
	fillSweepSkips(work, &result)
	sweepErr := executeSweepCandidates(ctx, cmd, work.cfg, work.root, work.actionable, &result)
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

// emitSweepReport prints the read-only view: the banner, then a line per
// workitem naming what would happen or why nothing will. It is what camp fresh
// prints by default on a non-TTY, so an agent reading a fresh transcript learns
// both the actionable set and the reasons for every non-move.
func emitSweepReport(ctx context.Context, out io.Writer, work *sweepWork) error {
	if banner := wkitem.SweepBannerText(len(work.actionable)); banner != "" {
		if _, err := fmt.Fprintln(out, banner); err != nil {
			return err
		}
	}
	if err := triage.WriteBanner(ctx, out, work.root, time.Now()); err != nil {
		return err
	}
	for _, cand := range work.actionable {
		verb := "promote to completed"
		if cand.Disposition == wkitem.DispositionRoute {
			verb = "route its findings"
		}
		if _, err := fmt.Fprintf(out, "  %s %s (%s): would %s\n",
			ui.InfoIcon(), filepath.ToSlash(cand.Item.RelativePath), cand.Item.WorkflowType, verb); err != nil {
			return err
		}
	}
	for _, skip := range work.skipped {
		if _, err := fmt.Fprintf(out, "  %s %s (%s): not moved, %s\n",
			ui.InfoIcon(), filepath.ToSlash(skip.Item.RelativePath), skip.Item.WorkflowType, skip.Detail); err != nil {
			return err
		}
	}
	return nil
}

func emitSweepResultWithNotice(ctx context.Context, cmd *cobra.Command, work *sweepWork, result *workitemSweepResult) error {
	if err := emitSweepResult(cmd, result, false); err != nil {
		return err
	}
	return triage.WriteBanner(ctx, cmd.OutOrStdout(), work.root, time.Now())
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
		if len(result.Items) == 0 && len(result.Skipped) == 0 {
			_, err := fmt.Fprintln(out, "No workitems with completed runs to sweep.")
			return err
		}
		if len(result.Items) > 0 {
			if _, err := fmt.Fprintf(out, "Sweep plan (%d workitem(s) with completed runs):\n", len(result.Items)); err != nil {
				return err
			}
			for _, it := range result.Items {
				dest := it.To
				if dest == "" {
					dest = "a destination you choose (camp workitem sweep --prompt)"
				}
				if _, err := fmt.Fprintf(out, "  %s (%s) -> %s\n", it.From, it.Type, dest); err != nil {
					return err
				}
			}
		}
		return printSweepSkips(out, result)
	}

	if result.Candidates == 0 && len(result.Skipped) == 0 {
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
	if err := printSweepSkips(out, result); err != nil {
		return err
	}
	if result.Candidates == 0 {
		return nil
	}
	summary := fmt.Sprintf("%s Swept %d workitem(s) to completed\n", ui.SuccessIcon(), result.Swept)
	if result.Failed > 0 {
		summary = fmt.Sprintf("%s %d swept, %d failed\n", ui.WarningIcon(), result.Swept, result.Failed)
	}
	_, err := fmt.Fprint(out, summary)
	return err
}

// printSweepSkips names every workitem the sweep declined to move and why, so
// the automatic path never leaves a decision unexplained.
func printSweepSkips(out io.Writer, result *workitemSweepResult) error {
	for _, s := range result.Skipped {
		if _, err := fmt.Fprintf(out, "  %s %s (%s): not moved, %s\n",
			ui.InfoIcon(), s.From, s.Type, s.Detail); err != nil {
			return err
		}
	}
	return nil
}
