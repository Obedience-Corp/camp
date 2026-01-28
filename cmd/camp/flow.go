package main

import (
	"github.com/spf13/cobra"
)

var flowCmd = &cobra.Command{
	Use:     "flow",
	Aliases: []string{"workflow", "wf"},
	Short:   "Manage status workflows for organizing work",
	Long: `Manage status workflows for organizing work.

A workflow defines status directories that items can move between,
with optional transition rules and history tracking. The workflow is
configured via a .workflow.yaml file.

GETTING STARTED:
  camp flow init              Initialize a new workflow
  camp flow sync              Create missing directories from schema
  camp flow status            Show workflow statistics

MANAGING ITEMS:
  camp flow list              List items in a status
  camp flow move <item> <to>  Move an item to a new status
  camp flow crawl             Interactive review of items

OTHER COMMANDS:
  camp flow show              Display workflow structure
  camp flow history           View transition history
  camp flow migrate           Upgrade legacy dungeon structure

DEFAULT STRUCTURE:
  active/                Work in progress
  ready/                 Prepared for action
  dungeon/
    completed/           Successfully finished
    archived/            Preserved but inactive
    someday/             Maybe later

Customize by editing .workflow.yaml and running 'camp flow sync'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(flowCmd)
	flowCmd.GroupID = "planning"
}
