//go:build dev

package main

import (
	"github.com/Obedience-Corp/camp/internal/commands/release"
	"github.com/spf13/cobra"
)

var questCmd = &cobra.Command{
	Use:   "quest",
	Short: "Manage quest execution contexts",
	Long: `Manage quest execution contexts inside a campaign.

Quests represent focused execution contexts rather than planning structures.
Planning remains in festivals; quests track the current working context under
.campaign/quests/.

Examples:
  camp quest create "refactor-runtime" --purpose "stabilize scheduler"
  camp quest list
  camp quest show qst_default
  camp quest pause qst_20260313_abc123
  camp quest restore qst_20260313_abc123`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	release.MarkDevOnly(questCmd)
	questCmd.GroupID = "planning"
	rootCmd.AddCommand(questCmd)
}
