package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/triage"
	"github.com/Obedience-Corp/camp/internal/triage/scaffold"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

// StartJSONVersion is the schema version of `camp triage start --json`.
const StartJSONVersion = "triage-start/v1alpha1"

// startResult is the `--json` payload. Snake-case single object, matching the
// rest of camp's machine-readable surfaces.
type startResult struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	Mode          string `json:"mode"`
	Profile       string `json:"profile"`
	Rows          int    `json:"rows"`
	Batches       int    `json:"batches"`
	// Queued and Carried always sum to Rows. Carried is 0 until carry-forward
	// lands; the field exists now so consumers do not have to change shape
	// when it starts moving.
	Queued  int `json:"queued"`
	Carried int `json:"carried"`
	// CarryLosses names every row that could have carried and did not, with
	// the reason. A row silently dropping back into review is what makes an
	// incremental run feel arbitrary.
	CarryLosses        []triage.CarryLoss `json:"carry_losses"`
	IdentityExceptions int                `json:"identity_exceptions"`
	// Repaired lists identities the preflight created. Always present so a
	// consumer never has to distinguish absent from empty.
	Repaired            []triage.Repair `json:"repaired"`
	RunDir              string          `json:"run_dir"`
	WorkflowDoc         string          `json:"workflow_doc,omitempty"`
	ScaffoldWorkflowDoc bool            `json:"scaffold_workflow_doc"`
	// DriverDoc is the generated agent brief for this run.
	DriverDoc string `json:"driver_doc"`
}

type startOptions struct {
	full     bool
	scope    []string
	jsonOut  bool
	noWorkfl bool
	identity string
	profile  string
}

func newStartCommand() *cobra.Command {
	opts := &startOptions{}

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Snapshot the campaign and open a triage run",
		Long: `Snapshot the campaign's workitems and open a triage run.

The snapshot is frozen: the run records what the campaign contained when it
started, along with the resolved profile it will be judged under, so a verdict
stays explainable even after the campaign and the profile move on.

Scope expressions use the same filters as camp workitem, one per --scope flag:

  --scope type:design            only design workitems
  --scope tag:launch             only items tagged launch
  --scope path:workflow/design   only items under a path (glob)

Available keys: type, category, status, stage, attention-stage, group, tag,
project, query, path.

Refuses (exit 2) when a run is already in progress; close it with
camp triage abandon first.`,
		Args: jsoncontract.Args(StartJSONVersion, func() bool { return opts.jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Deterministic snapshot with --json output; calls no models",
		},
		RunE: jsoncontract.RunE(StartJSONVersion, func() bool { return opts.jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runStart(cmd, opts)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(StartJSONVersion, func() bool { return opts.jsonOut }))

	f := cmd.Flags()
	f.BoolVar(&opts.full, "full", false, "Re-review every row instead of carrying unchanged verdicts forward")
	f.StringArrayVar(&opts.scope, "scope", nil, "Limit the run with a key:value filter (repeat for more)")
	f.BoolVar(&opts.jsonOut, "json", false, "Output result as a single JSON object")
	f.BoolVar(&opts.noWorkfl, "no-workflow-doc", false, "Skip the companion WORKFLOW.md scaffold")
	f.StringVar(&opts.identity, "identity", "",
		"Override the profile's identity policy: repair (adopt and report) or strict (refuse and list)")
	cmd.Flags().StringVar(&opts.profile, "profile", "",
		"Use a named built-in profile instead of the campaign's: default, sweep, or deep")
	return cmd
}

func runStart(cmd *cobra.Command, opts *startOptions) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Scaffold BEFORE resolving. A first run must resolve against the files it
	// is about to leave behind, or it freezes an empty set of type policies
	// and every verdict it records carries no vocabulary — which then reads,
	// on the next run, as though the campaign deleted six dispositions.
	if _, err := scaffold.Ensure(ctx, root); err != nil {
		return err
	}

	resolution, err := triage.ResolveProfileNamed(ctx, root, opts.profile)
	if err != nil {
		return err
	}
	profile := resolution.Profile
	// Apply the override onto the profile before anything reads it, so the
	// profile embedded in the manifest is the policy the run actually used
	// rather than the one the file happened to say.
	if opts.identity != "" {
		profile.Preflight.Identity = triage.IdentityPolicy(opts.identity)
	}
	scope := triage.NewScope(profile)
	if err := scope.ApplyExpressions(opts.scope); err != nil {
		return err
	}

	allItems, err := discoverAll(ctx, root, cfg)
	if err != nil {
		return err
	}
	items := scope.Apply(allItems)
	if len(items) == 0 {
		// Blocking here rather than opening an empty run: an empty run would
		// occupy the single active slot and have to be abandoned before the
		// operator could correct the scope.
		return preconditionError(cmd, opts.jsonOut, jsoncontract.WithHint(triage.ErrNoRowsInScope,
			"widen or drop --scope, or check `camp workitem` for what this campaign contains"))
	}

	// Preflight before the snapshot freezes: a row identified only by its path
	// cannot be moved safely later, because the path is the thing a verdict
	// changes.
	now := triage.SystemClock()
	preflight, err := triage.Preflight(ctx, triage.PreflightInput{
		CampaignRoot: root,
		Items:        items,
		AllItems:     allItems,
		Policy:       profile.Preflight.Identity,
		Now:          now,
	})
	if err != nil {
		return preconditionError(cmd, opts.jsonOut, jsoncontract.WithHint(err,
			"run `camp workitem adopt <path>` for each, or use the repair identity policy"))
	}

	store := triage.NewStore(root, nil)
	manifest, err := triage.BuildManifest(triage.SnapshotInput{
		ProfileName: resolution.Name,
		Profile:     profile,
		Mode:        startMode(opts.full),
		Items:       items,
		// Frozen so refresh can reproduce this run's selection instead of
		// treating every out-of-scope item as a new discovery.
		ScopeExpressions: opts.scope,
		TypePolicies:     resolution.TypePolicies,
		Now:              now,
	})
	if err != nil {
		return err
	}

	// D4: carry the verdicts of the last closed run for every row nothing
	// touched, so the second triage of a campaign is small. --full skips it.
	carry, err := carryFromLastRun(ctx, store, root, cfg, manifest, resolution, opts.full)
	if err != nil {
		return err
	}

	run, err := store.CreateRun(ctx, manifest)
	if err == nil {
		// The snapshot is written, so the run is snapshotted. Without this the
		// machine never leaves `created` and apply is unreachable.
		err = store.AdvancePhase(ctx, run.ID, triage.PhaseSnapshotted, "snapshot written")
	}
	if err != nil {
		if camperrors.Is(err, camperrors.ErrConflict) {
			return preconditionError(cmd, opts.jsonOut, jsoncontract.WithHint(err,
				"run `camp triage abandon` to close the run in progress"))
		}
		return err
	}

	result := startResult{
		SchemaVersion:       StartJSONVersion,
		RunID:               run.ID,
		Mode:                string(manifest.Mode),
		Profile:             manifest.Profile.Name,
		Rows:                len(manifest.Rows),
		Batches:             triage.BatchCount(manifest),
		Queued:              len(manifest.Rows) - carriedCount(carry),
		Carried:             carriedCount(carry),
		CarryLosses:         carryLossesOf(carry),
		IdentityExceptions:  triage.IdentityExceptionCount(manifest),
		Repaired:            preflight.Repaired,
		RunDir:              relativeRunDir(root, run.Dir),
		ScaffoldWorkflowDoc: profile.Outputs.ScaffoldWorkflowDoc && !opts.noWorkfl,
	}
	if result.ScaffoldWorkflowDoc {
		written, err := triage.ScaffoldWorkflowDoc(ctx, run)
		if err != nil {
			return err
		}
		result.WorkflowDoc = relativeRunDir(root, written)
	}

	// The agent brief: a self-contained document any agentic CLI can execute
	// against this run, naming this run's id in every command.
	driver, err := triage.WriteDriver(ctx, run, profile)
	if err != nil {
		return err
	}
	result.DriverDoc = relativeRunDir(root, driver)

	if opts.jsonOut {
		return writeJSON(cmd.OutOrStdout(), result)
	}
	return writeStartText(cmd.OutOrStdout(), result)
}

// startMode reports the run mode.
//
// Every run is full today. Incremental means "carry verdicts forward from a
// base run", and carry-forward lands in a later sequence, so claiming
// incremental would put a mode in the manifest that nothing honors — and the
// schema rejects an incremental run with no base run precisely to stop that.
// --full is accepted now so the flag surface is stable; it becomes meaningful
// when there is something to carry.
func startMode(_ bool) triage.RunMode {
	return triage.RunModeFull
}

// relativeRunDir renders a run path relative to the campaign root, since that
// is how every other camp surface refers to campaign files.
func relativeRunDir(root, dir string) string {
	if rel, err := filepath.Rel(root, dir); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return dir
}

// discoverAll runs camp's normal discovery over the whole campaign.
//
// Scope is applied by the caller rather than here: the preflight needs every
// item to keep generated ids and refs unique campaign-wide, not merely unique
// among the rows this run happens to look at.
func discoverAll(ctx context.Context, root string, cfg *config.CampaignConfig) ([]workitem.WorkItem, error) {
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := workitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, camperrors.Wrap(err, "discovering work items")
	}

	// Attention stages live in the priority store, not on disk, so a raw
	// Discover walk reports every item as having none. Without this overlay a
	// manifest freezes empty attention stages, which silently defeats the
	// profile's evidence-depth-by-stage policy and stage-based grouping, and
	// makes verify compare a recorded stage against nothing.
	store, err := priority.Load(priority.StorePath(root))
	if err != nil {
		return nil, camperrors.Wrap(err, "loading the priority store")
	}
	return priority.Apply(store, items), nil
}

// preconditionError marks a refusal that is a precondition failure rather than
// a fault: spec doc 03 gives those exit code 2 so a script can tell "you have
// to do something first" from "this broke".
//
// It prints the reason itself. Returning a CommandError with a non-zero exit
// code puts main on the silent-exit path — right for a command relaying a
// child process's failure, wrong here, where the whole value of the refusal is
// telling the operator what to do next. The JSON path does not print: the
// envelope carries the same message and hint, and a second copy on stderr
// would corrupt output that a caller is parsing.
func preconditionError(cmd *cobra.Command, jsonOut bool, err error) error {
	return preconditionErrorFor(cmd, cmd.CommandPath(), jsonOut, err)
}

// preconditionErrorFor is preconditionError with an explicit command label.
func preconditionErrorFor(cmd *cobra.Command, label string, jsonOut bool, err error) error {
	if !jsonOut {
		out := cmd.ErrOrStderr()
		_, _ = fmt.Fprintf(out, "Error: %v\n", err)
		if hint := jsoncontract.Hint(err); hint != "" {
			_, _ = fmt.Fprintf(out, "Hint: %s\n", hint)
		}
	}
	return camperrors.NewCommand(label, 2, err.Error(), err)
}

func writeJSON(w io.Writer, payload any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func writeStartText(w io.Writer, result startResult) error {
	if _, err := fmt.Fprintf(w, "Started triage run %s (%s, profile %s)\n",
		result.RunID, result.Mode, result.Profile); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %d rows in %d batches\n", result.Rows, result.Batches); err != nil {
		return err
	}
	// Every repair is named. Camp adopted these directories without being
	// asked, so silence would leave the operator with markers they did not
	// write and no record of where they came from.
	if len(result.Repaired) > 0 {
		if _, err := fmt.Fprintf(w, "\nAdopted %d workitem(s) that had no .workitem marker:\n",
			len(result.Repaired)); err != nil {
			return err
		}
		for _, repair := range result.Repaired {
			if _, err := fmt.Fprintf(w, "  %s  id: %s  ref: %s\n",
				repair.RelPath, repair.ID, repair.Ref); err != nil {
				return err
			}
		}
	}
	if result.IdentityExceptions > 0 {
		if _, err := fmt.Fprintf(w,
			"  %d row(s) have no .workitem marker and triage by path only\n",
			result.IdentityExceptions); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "  run: %s\n", result.RunDir); err != nil {
		return err
	}
	if result.WorkflowDoc != "" {
		if _, err := fmt.Fprintf(w, "  steps: %s\n", result.WorkflowDoc); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\nNext: camp triage status\n")
	return err
}

func init() {
	Cmd.AddCommand(newStartCommand())
}

// carryFromLastRun folds the previous run's verdicts into a new manifest.
//
// Returns a nil result when there is nothing to carry from — a first-ever
// triage, or --full. Carrying is an optimization on top of a correct run, so
// anything that goes wrong reading the base run degrades to "carry nothing"
// rather than failing the start: the operator gets a full review, which is
// slower but never wrong.
func carryFromLastRun(
	ctx context.Context, store *triage.Store, root string, cfg *config.CampaignConfig,
	manifest *triage.Manifest, resolution *triage.ProfileResolution, full bool,
) (*triage.CarryForwardResult, error) {
	if full {
		return nil, nil
	}

	baseID, err := store.LatestRunID(ctx)
	if err != nil || baseID == "" {
		return nil, nil //nolint:nilerr // no base run is the first-triage case
	}
	base, err := store.OpenRun(ctx, baseID)
	if err != nil || base.Manifest == nil {
		return nil, nil //nolint:nilerr // an unreadable base carries nothing
	}
	baseVerdicts, err := store.Verdicts(ctx, baseID)
	if err != nil {
		return nil, nil //nolint:nilerr
	}

	// Classify the new rows against the base run's snapshot, reusing the
	// refresh machinery rather than inventing a second notion of "unchanged".
	items, err := discoverAll(ctx, root, cfg)
	if err != nil {
		return nil, err
	}
	index := triage.IndexDiscovery(items)
	anchors, err := store.AnchorsByRow(ctx, baseID, rowIDsOf(base.Manifest.Rows))
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	checks, err := triage.CheckLocalAnchors(ctx, root, anchors, index)
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	diff := triage.ClassifyRows(triage.DiffInput{
		Rows: base.Manifest.Rows, Discovered: index.ByStableID, Anchors: checks,
	})

	classes := make(map[string]triage.RowClass, len(diff.Rows))
	for _, row := range diff.Rows {
		classes[row.StableID] = row.Class
	}

	result := triage.CarryForward(triage.CarryForwardInput{
		BaseRunID:    baseID,
		Rows:         manifest.Rows,
		BaseRows:     base.Manifest.Rows,
		BaseVerdicts: baseVerdicts,
		Classes:      classes,
		BaseProfile:  base.Manifest.Profile.Resolved,
		NextProfile:  resolution.Profile,
		BasePolicies: base.Manifest.Profile.TypePolicies,
		NextPolicies: resolution.TypePolicies,
	})
	manifest.BaseRunID = &baseID
	return &result, nil
}

// rowIDsOf lists manifest rows by stable id.
func rowIDsOf(rows []triage.ManifestRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.StableID)
	}
	return out
}

// carriedCount is how many verdicts survived into this run.
func carriedCount(carry *triage.CarryForwardResult) int {
	if carry == nil {
		return 0
	}
	return len(carry.Carried)
}

// carryLossesOf names every row that could have carried and did not, with the
// reason. A row silently dropping back into review is the thing that makes an
// incremental run feel arbitrary.
func carryLossesOf(carry *triage.CarryForwardResult) []triage.CarryLoss {
	if carry == nil {
		return []triage.CarryLoss{}
	}
	return carry.Losses
}
