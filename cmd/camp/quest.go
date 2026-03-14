//go:build dev

package main

import (
	"github.com/Obedience-Corp/camp/internal/commands/release"
	"github.com/spf13/cobra"
)

var questCmd = &cobra.Command{
	Use:   "quest",
	Short: "Manage working contexts within a campaign",
	Long: `Manage working contexts within a campaign.

A quest is a long-lived working context — a sub-initiative that may span
multiple projects, agent sessions, documents, and festivals over days or
weeks. Think of it as the current "what am I working on" scope, not a
single task or feature ticket.

Quests live under .campaign/quests/ and are orthogonal to planning (which
lives in festivals). A quest groups related activity; a festival plans and
executes specific deliverables within that activity.

Examples:
  camp quest create "platform-launch" --purpose "get v1 out the door"
  camp quest create "observability-overhaul" --no-editor
  camp quest list
  camp quest pause platform-launch
  camp quest complete observability-overhaul`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	release.MarkDevOnly(questCmd)
	questCmd.GroupID = "campaign"
	rootCmd.AddCommand(questCmd)
}
