package triage

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// ApplyJSONVersion is the schema version of `camp triage apply --json`.
const ApplyJSONVersion = "triage-apply/v1alpha1"

type applyResult struct {
	SchemaVersion string              `json:"schema_version"`
	RunID         string              `json:"run_id"`
	DryRun        bool                `json:"dry_run"`
	Plan          *triage.ApplyPlan   `json:"plan"`
	Applied       []string            `json:"applied"`
	Failed        string              `json:"failed,omitempty"`
	Skipped       []triage.SkippedRow `json:"skipped"`
	Receipts      []triage.Receipt    `json:"receipts"`
	Halted        bool                `json:"halted"`
}

func newApplyCommand() *cobra.Command {
	var (
		jsonOut bool
		dryRun  bool
		force   bool
		runID   string
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Execute the approved verdicts of the active run",
		Long: `Execute approved verdicts through camp's own workitem commands.

Apply refreshes first, every time. A verdict rests on facts that may have moved
since it was recorded, and applying over a stale one is the failure this whole
command exists to prevent. Rows the refresh did not return fresh or moved are
refused and listed; re-judge them and approve again.

Nothing here moves a directory itself. Attention changes go through the same
priority store camp workitem stage writes, and promotions run the same function
camp workitem promote runs, so a triage apply and a hand-typed command cannot
diverge.

Every action appends a receipt: what ran, how long it took, what it produced,
the commit it made, and the command that reverses it. The undo is derived from
where the workitem actually landed, not from where the plan expected it to.

A failure stops the pass. Rows after it stay pending rather than being applied
against a campaign that is no longer in the state the plan was compiled for.
Re-running continues from the first row without an applied receipt, so an
interrupted apply is resumed rather than restarted.

--dry-run prints the whole plan, including rows that are blocked, and changes
nothing. It does not require freshness, so it is safe to read at any time.`,
		Args: jsoncontract.Args(ApplyJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "false",
			"agent_reason":  "Moves real workitems; terminal actions require recorded human approval (D5)",
		},
		RunE: jsoncontract.RunE(ApplyJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runApply(cmd, applyOptions{
					jsonOut: jsonOut, dryRun: dryRun, force: force, runID: runID,
				})
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(ApplyJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the plan and change nothing")
	cmd.Flags().BoolVar(&force, "force", false,
		"Apply terminal actions whose anchors could not be re-checked")
	cmd.Flags().StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

type applyOptions struct {
	jsonOut bool
	dryRun  bool
	force   bool
	runID   string
}

func runApply(cmd *cobra.Command, opts applyOptions) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	store := triage.NewStore(root, nil)

	runID, err := resolveRunID(ctx, store, opts.runID)
	if err != nil {
		return err
	}

	run, err := store.OpenRun(ctx, runID)
	if err != nil {
		return err
	}
	verdicts, err := store.Verdicts(ctx, runID)
	if err != nil {
		return err
	}

	// Successors come from each consolidate verdict's rationale, which is
	// where the argument for retiring the parent lives.
	successors, err := store.SuccessorsByRow(ctx, runID, verdicts)
	if err != nil {
		return err
	}
	plan, err := triage.CompilePlan(triage.CompileInput{
		RunID:      runID,
		Rows:       run.Manifest.Rows,
		Verdicts:   verdicts,
		Successors: successors,
		// `camp workitem split` landed in phase 004, so consolidate verdicts
		// now compile to real commands rather than a blocked entry.
		SplitAvailable: true,
		Now:            triage.SystemClock(),
	})
	if err != nil {
		return err
	}

	out := applyResult{
		SchemaVersion: ApplyJSONVersion,
		RunID:         runID,
		DryRun:        opts.dryRun,
		Plan:          plan,
		Skipped:       []triage.SkippedRow{},
		Receipts:      []triage.Receipt{},
	}

	// A dry-run is a read. It deliberately does not refresh: the point is to
	// show what would run, and making it require network or a fresh walk
	// would stop it being the cheap thing an operator reaches for.
	if opts.dryRun {
		if opts.jsonOut {
			return writeJSON(cmd.OutOrStdout(), out)
		}
		return printPlan(cmd.OutOrStdout(), plan)
	}

	readiness, err := refreshForApply(cmd, store, root, cfg, runID, verdicts, opts.force)
	if err != nil {
		return err
	}

	result, err := store.Apply(ctx, triage.ApplyInput{
		RunID:     runID,
		Plan:      plan,
		Mover:     &serviceMover{campaignRoot: root, cfg: cfg, out: io.Discard},
		Actor:     triage.ResolveActor(ctx),
		Now:       triage.SystemClock,
		Readiness: readiness,
	})
	if err != nil {
		return err
	}

	out.Applied = result.Applied
	out.Failed = result.Failed
	out.Halted = result.Halted
	if result.Skipped != nil {
		out.Skipped = result.Skipped
	}
	if result.Receipts != nil {
		out.Receipts = result.Receipts
	}

	if opts.jsonOut {
		if err := writeJSON(cmd.OutOrStdout(), out); err != nil {
			return err
		}
	} else if err := printApply(cmd.OutOrStdout(), result); err != nil {
		return err
	}

	// Exit 2 when nothing could be applied because rows were refused: the
	// operator has to do something first, which is a different outcome from
	// a clean run and from a crash.
	if len(result.Applied) == 0 && len(result.Skipped) > 0 && !result.Halted {
		return preconditionErrorFor(cmd, ApplyJSONVersion, opts.jsonOut,
			jsoncontract.WithHint(
				camperrors.Wrap(camperrors.ErrNotFound, "no rows were applied"),
				"re-judge the rows the refresh staled, then camp triage approve"))
	}

	// A halt is a failure, and has to exit like one. Exiting 0 made a stopped
	// apply indistinguishable from a finished one to anything reading the
	// status code — a script, a CI step, or the operator's own `&&` — while
	// the run went on to verify clean and report itself verified.
	if result.Halted {
		return preconditionErrorFor(cmd, ApplyJSONVersion, opts.jsonOut,
			jsoncontract.WithHint(
				camperrors.Wrapf(camperrors.ErrConflict,
					"apply stopped at %s with %d row(s) applied", result.Failed, len(result.Applied)),
				"the receipt for "+result.Failed+" records the cause; fix it and re-run "+
					"`camp triage apply`, which continues from where it stopped"))
	}
	return nil
}

// refreshForApply runs the implicit refresh and turns it into apply readiness.
func refreshForApply(
	cmd *cobra.Command, store *triage.Store, root string, cfg *config.CampaignConfig,
	runID string, verdicts map[string]triage.RowVerdict, force bool,
) (map[string]triage.ApplyReadiness, error) {
	ctx := cmd.Context()

	items, err := discoverAll(ctx, root, cfg)
	if err != nil {
		return nil, err
	}
	var remote triage.RemoteChecker
	if checker, err := triage.NewGHRemoteChecker(); err == nil {
		remote = checker
	}

	refreshed, err := store.Refresh(ctx, triage.RefreshInput{
		RunID: runID, Items: items, Actor: triage.ResolveActor(ctx),
		Now: triage.SystemClock(), Remote: remote,
	})
	if err != nil {
		return nil, err
	}

	actions := make(map[string]triage.CanonicalAction, len(verdicts))
	for id, verdict := range verdicts {
		actions[id] = verdict.CanonicalAction
	}
	return triage.ApplyReadinessFromDiff(refreshed.Diff, actions, force), nil
}

// printPlan renders a dry-run: every entry, including the blocked ones.
func printPlan(w io.Writer, plan *triage.ApplyPlan) error {
	if len(plan.Entries) == 0 {
		_, err := fmt.Fprint(w,
			"Nothing to apply: no row carries an approved verdict.\n"+
				"Approve some with camp triage approve.\n")
		return err
	}

	if _, err := fmt.Fprintf(w, "Plan for %s — %s\n\n",
		plan.RunID, entryCountLine(plan)); err != nil {
		return err
	}
	for _, entry := range plan.Entries {
		if entry.Blocked != "" {
			if _, err := fmt.Fprintf(w, "  [blocked] %s\n    %s\n\n",
				entry.StableID, entry.Blocked); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "  %s\n", entry.StableID); err != nil {
			return err
		}
		for _, command := range entry.Commands {
			if _, err := fmt.Fprintf(w, "    run:  %s\n", command.String()); err != nil {
				return err
			}
		}
		if len(entry.Undo) > 0 {
			if _, err := fmt.Fprintf(w, "    undo: %s\n", joinArgv(entry.Undo)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}

	_, err := fmt.Fprint(w, "Nothing was changed. Run without --dry-run to apply.\n")
	return err
}

// entryCountLine summarizes a plan's size.
func entryCountLine(plan *triage.ApplyPlan) string {
	runnable := len(plan.ExecutableEntries())
	blocked := len(plan.BlockedEntries())
	line := strconv.Itoa(runnable) + " to run"
	if blocked > 0 {
		line += ", " + strconv.Itoa(blocked) + " blocked"
	}
	return line
}

// printApply renders what actually happened.
func printApply(w io.Writer, result *triage.ApplyResult) error {
	if _, err := fmt.Fprintf(w, "Applied %d row(s) in %s\n",
		len(result.Applied), result.RunID); err != nil {
		return err
	}
	for _, receipt := range result.Receipts {
		if receipt.Result != "applied" {
			continue
		}
		if _, err := fmt.Fprintf(w, "  %s\n    ran:  %s\n",
			receipt.StableID, joinArgv(receipt.Argv)); err != nil {
			return err
		}
		if receipt.Undo != "" {
			if _, err := fmt.Fprintf(w, "    undo: %s\n", receipt.Undo); err != nil {
				return err
			}
		}
	}

	for _, skipped := range result.Skipped {
		if _, err := fmt.Fprintf(w, "\n  skipped %s\n    %s\n",
			skipped.StableID, skipped.Reason); err != nil {
			return err
		}
	}

	if result.Halted {
		_, err := fmt.Fprintf(w,
			"\nStopped at %s. The rows after it were not applied.\n"+
				"Fix the cause and re-run: apply continues from where it stopped.\n",
			result.Failed)
		return err
	}
	return nil
}

// joinArgv renders an argv for display.
func joinArgv(argv []string) string {
	out := ""
	for i, arg := range argv {
		if i > 0 {
			out += " "
		}
		out += arg
	}
	return out
}

func init() {
	Cmd.AddCommand(newApplyCommand())
}
