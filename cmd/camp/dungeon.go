package main

import (
	"github.com/spf13/cobra"
)

var dungeonCmd = &cobra.Command{
	Use:   "dungeon",
	Short: "Manage the campaign dungeon",
	Long: `Manage the campaign dungeon - a holding area for uncertain work.

The dungeon is where you put work you're unsure about or want out of the way.
It keeps items visible without them competing for your attention.

Commands:
  add     Initialize dungeon structure with documentation
  crawl   Interactive review and archival of dungeon contents

Examples:
  camp dungeon add            Initialize the dungeon
  camp dungeon crawl          Review and archive dungeon items`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(dungeonCmd)
	dungeonCmd.GroupID = "planning"
}
