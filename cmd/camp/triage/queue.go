package triage

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// QueueJSONVersion is the schema version of `camp triage queue --json`.
const QueueJSONVersion = "triage-queue/v1alpha1"

type queueResult struct {
	SchemaVersion string        `json:"schema_version"`
	Queue         *triage.Queue `json:"queue"`
}

func newQueueCommand() *cobra.Command {
	var (
		jsonOut bool
		role    string
		runID   string
	)

	cmd := &cobra.Command{
		Use:   "queue",
		Short: "List rows awaiting judgment",
		Long: `List the rows of the active run that are still awaiting judgment.

This is the dispatch surface for whatever drives the evidence phase. Each row
carries its batch, its type policy (how much evidence to gather, which model
class to read it with), and the schema version of the record to produce. An
orchestrating agent reads the queue, fans out however it likes, and submits
results with camp triage evidence set and camp triage propose.

Camp never calls a model. The routing block is advisory: camp passes it
through verbatim and does not enforce it.

Roles:
  evidence     the row has no evidence record yet
  synthesis    evidence exists, but no proposal does

Rows that already hold a proposal are not queued: what they need next is a
human approving them, not more judgment. Carried rows are not queued either -
their verdicts came forward precisely so nobody re-reads them.`,
		Args: jsoncontract.Args(QueueJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only work dispatch with --json output; calls no models",
		},
		RunE: jsoncontract.RunE(QueueJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runQueue(cmd, jsonOut, role, runID)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QueueJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().StringVar(&role, "role", "", "Only rows awaiting this role: evidence or synthesis")
	cmd.Flags().StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

func runQueue(cmd *cobra.Command, jsonOut bool, role, runID string) error {
	ctx := cmd.Context()

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}
	store := triage.NewStore(root, nil)

	runID, err = resolveRunID(ctx, store, runID)
	if err != nil {
		return err
	}

	queue, err := triage.BuildQueue(ctx, store, runID, triage.QueueRole(role))
	if err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), queueResult{
			SchemaVersion: QueueJSONVersion,
			Queue:         queue,
		})
	}
	return writeQueueText(cmd.OutOrStdout(), queue)
}

// resolveRunID falls back to the latest run when none was named.
func resolveRunID(ctx context.Context, store *triage.Store, runID string) (string, error) {
	if runID != "" {
		return runID, nil
	}
	return store.LatestRunID(ctx)
}

func writeQueueText(w io.Writer, queue *triage.Queue) error {
	if _, err := fmt.Fprintf(w, "Run %s (phase %s)\n", queue.RunID, queue.Phase); err != nil {
		return err
	}
	if len(queue.Items) == 0 {
		_, err := fmt.Fprintf(w, "\nNothing awaiting judgment.\n  %d of %d rows done.\n",
			queue.Counts.Done, queue.Counts.Total)
		return err
	}

	if _, err := fmt.Fprintf(w, "  awaiting evidence: %d   awaiting synthesis: %d   done: %d\n\n",
		queue.Counts.Evidence, queue.Counts.Synthesis, queue.Counts.Done); err != nil {
		return err
	}
	for _, item := range queue.Items {
		if _, err := fmt.Fprintf(w, "  [batch %d] %-10s %s\n    %s\n    evidence: %s  tier: %s\n",
			item.Batch, item.Role, item.StableID, item.RelativePath,
			item.Policy.Evidence, item.Policy.RoutingTier); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w,
		"\nSubmit with:\n  camp triage evidence set <stable-id> --file <record.json>\n"+
			"  camp triage propose <stable-id> --disposition <d>\n")
	return err
}

func init() {
	Cmd.AddCommand(newQueueCommand())
}
