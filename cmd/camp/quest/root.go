//go:build dev

package quest

import (
	"github.com/spf13/cobra"
)

// Cmd is the root quest command exposed for registration by the parent package.
var Cmd = &cobra.Command{
	Use:     "quest",
	Short:   "Manage working contexts within a campaign",
	GroupID: "campaign",
	Long: `Manage working contexts within a campaign.

A quest is a long-lived working context — a sub-initiative that may span
multiple projects, agent sessions, documents, and festivals over days or
weeks. Think of it as the current "what am I working on" scope, not a
single task or feature ticket.

Quests live under .campaign/quests/ and are orthogonal to planning (which
lives in festivals). A quest groups related activity; a festival plans and
executes specific deliverables within that activity.

Examples:
  camp quest create "q2-reliability" --purpose "harden platform for Q2 launch"
  camp quest create "data-pipeline-rethink" --no-editor
  camp quest list
  camp quest pause q2-reliability
  camp quest complete data-pipeline-rethink`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
