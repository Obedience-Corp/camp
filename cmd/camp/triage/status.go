package triage

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// StatusJSONVersion is the schema version of `camp triage status --json`.
const StatusJSONVersion = "triage-status/v1alpha1"

type statusResult struct {
	SchemaVersion string `json:"schema_version"`
	// HasRun is false for a campaign that has never run triage. The command
	// still exits 0: "no run yet" is an answer, not a failure.
	HasRun bool           `json:"has_run"`
	Run    *triage.Status `json:"run"`
	// StaleNotice is the same one-liner high-traffic commands print, omitted
	// when the last refresh is still fresh (or there has never been one).
	StaleNotice string `json:"stale_notice,omitempty"`
}

func newStatusCommand() *cobra.Command {
	var (
		jsonOut bool
		runID   string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show where the active triage run stands",
		Long: `Show where the active triage run stands.

Status reports the session, not the camp. It reads the run's own recorded
data and never walks the filesystem, so it is instant and keeps meaning even
after the camp moves underneath the run. Comparing a run against the
current state of the camp is what camp triage refresh does.

When the last refresh is older than the camp's runs.stale_after_days
threshold, or workitems have changed since, it also prints the same one-line
notice high-traffic commands share (from the cached verdict, not a discovery
walk).

Exits 0 when there is no run: a camp that has not triaged yet is a state,
not an error.`,
		Args: jsoncontract.Args(StatusJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only session status with --json output",
		},
		RunE: jsoncontract.RunE(StatusJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runStatus(cmd, jsonOut, runID)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(StatusJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().StringVar(&runID, "run", "", "Inspect a specific run id instead of the latest")
	return cmd
}

func runStatus(cmd *cobra.Command, jsonOut bool, runID string) error {
	ctx := cmd.Context()

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}
	store := triage.NewStore(root, nil)

	if runID == "" {
		runID, err = store.LatestRunID(ctx)
		if err != nil {
			var notFound *camperrors.NotFoundError
			if camperrors.As(err, &notFound) {
				return emitNoRun(cmd, jsonOut)
			}
			return err
		}
	}

	status, err := triage.BuildStatus(ctx, store, runID)
	if err != nil {
		return err
	}

	staleNotice := triage.BannerFor(ctx, root, time.Now())
	result := statusResult{
		SchemaVersion: StatusJSONVersion,
		HasRun:        true,
		Run:           status,
		StaleNotice:   staleNotice,
	}
	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), result)
	}
	return writeStatusText(cmd.OutOrStdout(), status, staleNotice)
}

// emitNoRun answers the never-triaged case without failing.
func emitNoRun(cmd *cobra.Command, jsonOut bool) error {
	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), statusResult{
			SchemaVersion: StatusJSONVersion,
			HasRun:        false,
		})
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(),
		"No triage run yet.\n\nStart one: camp triage start\n")
	return err
}

func writeStatusText(w io.Writer, status *triage.Status, staleNotice string) error {
	state := "active"
	if !status.Active {
		state = string(status.Phase)
	}
	if _, err := fmt.Fprintf(w, "Run %s (%s, profile %s)\n  phase: %s  [%s]\n",
		status.RunID, status.Mode, status.Profile, status.Phase, state); err != nil {
		return err
	}
	if status.AbandonReason != "" {
		if _, err := fmt.Fprintf(w, "  abandoned: %s\n", status.AbandonReason); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "  rows: %d\n", status.Rows); err != nil {
		return err
	}

	for _, state := range triage.RowStates() {
		count := status.Counts[state]
		if count == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "    %-16s %d\n", state, count); err != nil {
			return err
		}
	}

	if len(status.Batches) > 0 {
		if _, err := fmt.Fprintf(w, "  batches:\n"); err != nil {
			return err
		}
		for _, batch := range status.Batches {
			if _, err := fmt.Fprintf(w, "    %2d: %d/%d decided\n",
				batch.Batch, batch.Decided, batch.Rows); err != nil {
				return err
			}
		}
	}
	if status.IdentityIssues > 0 {
		if _, err := fmt.Fprintf(w,
			"  %d row(s) triage by path only (no .workitem marker)\n",
			status.IdentityIssues); err != nil {
			return err
		}
	}
	if len(status.Consolidations) > 0 {
		if _, err := fmt.Fprintf(w, "  consolidations pending: %d\n",
			len(status.Consolidations)); err != nil {
			return err
		}
	}
	if staleNotice != "" {
		if _, err := fmt.Fprintln(w, staleNotice); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	Cmd.AddCommand(newStatusCommand())
}
