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

// RefreshJSONVersion is the schema version of `camp triage refresh --json`.
const RefreshJSONVersion = "triage-refresh/v1alpha1"

type refreshResult struct {
	SchemaVersion string      `json:"schema_version"`
	RunID         string      `json:"run_id"`
	Rows          []refreshed `json:"rows"`
	// CarryLost names every carried verdict this refresh invalidated, with
	// the reason. Spec doc 04 requires a lost carry always be explainable.
	CarryLost []triage.CarryLoss `json:"carry_lost"`
	Summary   summary            `json:"summary"`
}

// refreshed is one row's classification, flattened for consumers: a class, the
// reason it got that class, and what it means for apply.
type refreshed struct {
	StableID string `json:"stable_id"`
	Class    string `json:"class"`
	Reason   string `json:"reason"`
	// Applicable reports whether apply may still execute this row. Carried
	// explicitly so a consumer does not have to re-derive the class rule.
	Applicable bool `json:"applicable"`
	// StaleRecorded reports that this refresh retired a live verdict here.
	StaleRecorded bool `json:"stale_recorded"`
	// Rekeyed reports that the manifest row was moved in place.
	Rekeyed bool `json:"rekeyed"`
	// Appended reports that this row was added to the manifest by this run.
	Appended         bool `json:"appended"`
	UncheckedAnchors int  `json:"unchecked_anchors"`
}

type summary struct {
	Fresh   int `json:"fresh"`
	Moved   int `json:"moved"`
	Changed int `json:"changed"`
	Gone    int `json:"gone"`
	New     int `json:"new"`
	// StaleRecorded counts verdicts this refresh retired.
	StaleRecorded int `json:"stale_recorded"`
	// RowsWithUncheckedAnchors counts rows carrying at least one anchor that
	// could not be re-checked. Reported separately from the classes because
	// "nothing changed" and "we could not look" are different claims.
	RowsWithUncheckedAnchors int `json:"rows_with_unchecked_anchors"`
	// RemoteAnchorsResolved counts anchors answered by the remote or its
	// cache rather than left unchecked.
	RemoteAnchorsResolved int `json:"remote_anchors_resolved"`
	// CarryLost counts carried verdicts invalidated by this refresh.
	CarryLost int `json:"carry_lost"`
}

func newRefreshCommand() *cobra.Command {
	var (
		jsonOut bool
		runID   string
	)

	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Re-check the active run against the world",
		Long: `Re-check every row against a fresh discovery pass and its evidence anchors.

Verdicts expire. A row judged an hour ago rested on facts — a file's contents, a
workitem's stage, a festival's status — and refresh is what notices when one of
them moved. Each row comes back in one of five classes:

  fresh    identity resolves and every anchor still matches; the verdict stands
  moved    the item is at a new path or stage; the row is re-keyed and the
           verdict stands, because identity survives moves
  changed  an anchor observes a different value; the verdict goes stale and the
           row returns to the judgment queue
  gone     the item is no longer discoverable outside dungeons; the verdict goes
           stale and the row is flagged — someone likely finished it elsewhere
  new      discovered but absent from the snapshot; appended and queued

Every row prints the reason for its class, naming the anchor or the location
that decided it.

Anchors that need the network are recorded unchecked rather than assumed
current, and the summary counts them separately: not knowing is reported as not
knowing.

Refresh only records. It retires verdicts, re-keys rows, and appends new ones —
it never moves a workitem. That is camp triage apply, which refuses any row this
command did not return fresh or moved.`,
		Args: jsoncontract.Args(RefreshJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-mostly re-check; writes only run bookkeeping and never moves a workitem",
		},
		RunE: jsoncontract.RunE(RefreshJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runRefresh(cmd, jsonOut, runID)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(RefreshJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

func runRefresh(cmd *cobra.Command, jsonOut bool, runID string) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	store := triage.NewStore(root, nil)

	runID, err = resolveRunID(ctx, store, runID)
	if err != nil {
		return err
	}

	// The whole campaign, unscoped: Refresh reapplies the run's own frozen
	// scope so this command cannot narrow it differently than start did.
	items, err := discoverAll(ctx, root, cfg)
	if err != nil {
		return err
	}

	// A missing gh is the offline case, not a failure: pr anchors record
	// unchecked-offline and the refresh reports how many rows that left
	// unverified. Nothing here waits on the network to answer a local question.
	var remote triage.RemoteChecker
	if checker, err := triage.NewGHRemoteChecker(); err == nil {
		remote = checker
	}

	profile := triage.DefaultProfile()
	result, err := store.Refresh(ctx, triage.RefreshInput{
		RunID:          runID,
		Items:          items,
		Actor:          triage.ResolveActor(ctx),
		Now:            triage.SystemClock(),
		Remote:         remote,
		CurrentProfile: &profile,
	})
	if err != nil {
		return err
	}

	out := buildRefreshResult(result)
	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), out)
	}
	return printRefresh(cmd.OutOrStdout(), out)
}

// buildRefreshResult flattens the store's result into the output shape.
func buildRefreshResult(result *triage.RefreshResult) refreshResult {
	stale := setOf(result.StaleRecorded)
	rekeyed := setOf(result.Rekeyed)
	appended := setOf(result.Appended)

	rows := make([]refreshed, 0, len(result.Diff.Rows))
	for _, row := range result.Diff.Rows {
		rows = append(rows, refreshed{
			StableID:         row.StableID,
			Class:            string(row.Class),
			Reason:           row.Reason,
			Applicable:       row.Class.Applicable(),
			StaleRecorded:    stale[row.StableID],
			Rekeyed:          rekeyed[row.StableID],
			Appended:         appended[row.StableID],
			UncheckedAnchors: row.UncheckedAnchors,
		})
	}

	counts := result.Diff.CountByClass()
	losses := result.CarryLost
	if losses == nil {
		losses = []triage.CarryLoss{}
	}
	return refreshResult{
		SchemaVersion: RefreshJSONVersion,
		RunID:         result.RunID,
		Rows:          rows,
		CarryLost:     losses,
		Summary: summary{
			Fresh:                    counts[triage.ClassFresh],
			Moved:                    counts[triage.ClassMoved],
			Changed:                  counts[triage.ClassChanged],
			Gone:                     counts[triage.ClassGone],
			New:                      counts[triage.ClassNew],
			StaleRecorded:            len(result.StaleRecorded),
			RowsWithUncheckedAnchors: result.RowsWithUncheckedAnchors(),
			RemoteAnchorsResolved:    result.RemoteResolved,
			CarryLost:                len(result.CarryLost),
		},
	}
}

// printRefresh writes the human report: the counts, then every row that is not
// simply fresh, with its reason.
//
// Fresh rows are summarized rather than listed. On a 500-row run the answer
// worth reading is what moved, and printing 480 lines of "still fine" would
// bury it.
func printRefresh(w io.Writer, result refreshResult) error {
	s := result.Summary
	if _, err := fmt.Fprintf(w, "Refreshed %s\n  %s\n",
		result.RunID, classLine(s)); err != nil {
		return err
	}

	for _, row := range result.Rows {
		if row.Class == string(triage.ClassFresh) {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n  %-7s %s\n    %s\n",
			row.Class, row.StableID, row.Reason); err != nil {
			return err
		}
		if row.StaleRecorded {
			if _, err := fmt.Fprint(w,
				"    verdict retired; the row is back in camp triage queue\n"); err != nil {
				return err
			}
		}
	}

	for _, loss := range result.CarryLost {
		if _, err := fmt.Fprintf(w, "\n  carry   %s\n    %s\n",
			loss.StableID, loss.Reason); err != nil {
			return err
		}
	}

	if s.RowsWithUncheckedAnchors > 0 {
		if _, err := fmt.Fprintf(w,
			"\n%d row(s) carry an anchor that could not be re-checked.\n"+
				"They are reported unchecked rather than assumed current.\n",
			s.RowsWithUncheckedAnchors); err != nil {
			return err
		}
	}
	// Gated on what was actually retired, not on how many rows changed. A
	// changed row nobody had judged retires nothing, and "0 verdict(s) went
	// stale" is a line that makes a reader doubt the rest of the output.
	if s.StaleRecorded > 0 {
		if _, err := fmt.Fprintf(w,
			"\n%d verdict(s) went stale. Re-judge them, then approve again:\n"+
				"  camp triage queue\n", s.StaleRecorded); err != nil {
			return err
		}
	}
	return nil
}

// classLine renders the per-class counts on one line, naming every class even
// at zero so the shape of the output does not change run to run.
func classLine(s summary) string {
	return "fresh " + strconv.Itoa(s.Fresh) +
		" · moved " + strconv.Itoa(s.Moved) +
		" · changed " + strconv.Itoa(s.Changed) +
		" · gone " + strconv.Itoa(s.Gone) +
		" · new " + strconv.Itoa(s.New)
}

// setOf indexes a list of stable ids for membership tests.
func setOf(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func init() {
	Cmd.AddCommand(newRefreshCommand())
}
