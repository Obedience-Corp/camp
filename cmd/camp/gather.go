package main

import (
	"github.com/spf13/cobra"
)

var gatherCmd = &cobra.Command{
	Use:     "gather",
	Short:   "Import external data into the intent system",
	GroupID: "planning",
	Long: `Gather external data sources into trackable intents.

The gather command imports data from various sources into the intent system,
creating structured intents with checkboxes for tracking progress.

Available sources:
  feedback    Gather feedback observations from festivals`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(gatherCmd)
}
