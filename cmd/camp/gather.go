package main

import (
	"github.com/spf13/cobra"

	workitemcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
)

var gatherCmd = &cobra.Command{
	Use:     "gather",
	Short:   "Gather related work into unified items",
	GroupID: "planning",
	Long: `Gather related work into unified items.

Available sources:
  feedback    Import feedback observations from festivals into intents
  design      Combine selected design workitems into one gathered package

For gathering intents by tag, hashtag, or similarity, see 'camp intent gather'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(gatherCmd)
	gatherCmd.AddCommand(workitemcmd.NewGatherCommand("design"))
}
