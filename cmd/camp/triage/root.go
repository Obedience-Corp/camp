// Package triage registers `camp triage`: a recorded, resumable review of a
// campaign's workitems.
//
// The commands here are a thin shell. Scope resolution, snapshotting, and the
// run store all live in internal/triage so they are testable without a
// process, and so the JSON contract and the Go API cannot drift apart.
package triage

import "github.com/spf13/cobra"

// Cmd is the root of the triage command family.
var Cmd = &cobra.Command{
	Use:     "triage",
	Short:   "Review the campaign's workitems in a recorded session",
	GroupID: "planning",
	Long: `Review the campaign's workitems in a recorded, resumable session.

A triage run freezes what the campaign contains, collects evidence about each
item, records your verdicts, and applies them through camp's normal workitem
machinery. Every step is written to .campaign/triage/runs/<run-id>/, so a run
survives being interrupted and the decisions stay auditable afterwards.

Camp never calls a model. Agents read the queue and submit evidence and
proposals; you approve them; camp applies what you approved.

Session:
  start     Snapshot the campaign and open a run
  status    Show where the active run stands
  abandon   Close the active run without applying it

Judgment:
  queue     List rows awaiting evidence or a proposal
  evidence  Submit a record, or print one with the known facts filled in
  propose   Propose a disposition for a row

The judgment commands are the driver seam: camp says what needs judging and
under what policy, anything you like does the judging, and camp validates what
comes back. A run leaves the judging phase only once every row holds evidence
and a proposal, or is explicitly marked judged without a record.

Examples:
  camp triage start                        Start a run over everything in scope
  camp triage start --scope type:design    Limit the run to design workitems
  camp triage status --json                Inspect the active run

  camp triage queue --json                 What still needs judging
  camp triage evidence template <id>       A record with camp's facts filled in
  camp triage evidence set <id> --file r.json
  camp triage propose <id> --disposition completed --summary "shipped in #239"

  camp triage abandon --reason "wrong scope"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
