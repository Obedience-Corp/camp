package triage

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// AbandonJSONVersion is the schema version of `camp triage abandon --json`.
const AbandonJSONVersion = "triage-abandon/v1alpha1"

type abandonResult struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	Phase         string `json:"phase"`
	Reason        string `json:"reason,omitempty"`
	RunDir        string `json:"run_dir"`
}

func newAbandonCommand() *cobra.Command {
	var (
		jsonOut bool
		reason  string
		runID   string
	)

	cmd := &cobra.Command{
		Use:   "abandon",
		Short: "Close the active triage run without applying it",
		Long: `Close the active triage run without applying it.

Nothing is deleted. The run keeps its snapshot, its evidence, and every verdict
recorded so far; only its phase changes, which frees the active slot so a new
run can start. An abandoned run is still readable, and still the record of what
was decided before it was set aside.

A reason is optional but worth giving: it is what explains the abandonment to
whoever reads the run later.`,
		Args: jsoncontract.Args(AbandonJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "false",
			"agent_reason":  "Closes a human's review session; the operator decides when a run is over",
		},
		RunE: jsoncontract.RunE(AbandonJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runAbandon(cmd, jsonOut, reason, runID)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(AbandonJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().StringVar(&reason, "reason", "", "Why the run is being abandoned")
	cmd.Flags().StringVar(&runID, "run", "", "Abandon a specific run id instead of the latest")
	return cmd
}

func runAbandon(cmd *cobra.Command, jsonOut bool, reason, runID string) error {
	ctx := cmd.Context()

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	store := triage.NewStore(root, nil)

	if runID == "" {
		runID, err = store.LatestRunID(ctx)
		if err != nil {
			var notFound *camperrors.NotFoundError
			if camperrors.As(err, &notFound) {
				return preconditionErrorFor(cmd, "camp triage abandon", jsonOut,
					jsoncontract.WithHint(err, "there is no triage run to abandon"))
			}
			return err
		}
	}

	run, err := store.Abandon(ctx, runID, reason)
	if err != nil {
		return err
	}

	result := abandonResult{
		SchemaVersion: AbandonJSONVersion,
		RunID:         run.ID,
		Phase:         string(run.State.Phase),
		RunDir:        relativeRunDir(root, run.Dir),
	}
	if run.State.AbandonReason != nil {
		result.Reason = *run.State.AbandonReason
	}

	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), result)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(),
		"Abandoned run %s\n  state kept at %s\n\nStart a new one: camp triage start\n",
		result.RunID, result.RunDir)
	return err
}

func init() {
	Cmd.AddCommand(newAbandonCommand())
}
