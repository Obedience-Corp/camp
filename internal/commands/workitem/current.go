package workitem

import (
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

// CurrentRemovedSchemaVersion is the schema version used by the removed
// `camp workitem current` command's JSON error envelope. Kept so legacy
// automation that requested --json still receives a structured refusal
// rather than a list payload from the parent command.
const CurrentRemovedSchemaVersion = "workitem-current/v1alpha1"

const currentRemovedMessage = "camp workitem current was removed"

const currentRemovedHint = `Scope a workitem with one of:
  --workitem <selector>            explicit workitem for commit/resolve
  cd workflow/<type>/<slug> && ...  run from inside a workitem directory
  camp workitem link <id> <scope>  primary-link a project/festival for cwd resolution

Do not rely on a campaign-local current.yaml pointer; that selection is no longer read.`

// newCurrentCommand is a tombstone for the removed local current-workitem
// pointer. It must remain registered so legacy argv
// (`camp workitem current --json`) cannot fall through to the parent list
// command and silently emit a workitems/v1alpha10 payload.
func newCurrentCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		// Do not set Deprecated: cobra prints a non-JSON warning to stderr
		// before RunE, which would break the structured --json refusal.
		Use:   "current [selector]",
		Short: "Removed: was the local current-workitem pointer",
		Long: `camp workitem current has been removed.

It previously stored a single campaign-local pointer in
.campaign/workitems/current.yaml. That model collided with multi-item
attention stages and was easy for humans and agents to misuse when stale.

Scope workitems with --workitem, by working inside a workitem directory, or
with a primary project/festival link (camp workitem link).`,
		// Accept the old flag/arg shapes so they still hit this tombstone.
		Args: jsoncontract.Args(CurrentRemovedSchemaVersion, func() bool { return jsonOut }, cobra.ArbitraryArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Tombstone: rejects legacy current selection with structured --json migration error",
		},
		RunE: jsoncontract.RunE(CurrentRemovedSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return currentRemovedError()
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(CurrentRemovedSchemaVersion, func() bool { return jsonOut }))
	// Keep --clear so legacy scripts that pass it still land here rather than
	// failing flag parsing before the removal message.
	cmd.Flags().Bool("clear", false, "removed: previously cleared current.yaml")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON error envelope")
	return cmd
}

func currentRemovedError() error {
	return jsoncontract.WithHint(
		camperrors.NewValidation("current", currentRemovedMessage, nil),
		currentRemovedHint,
	)
}
