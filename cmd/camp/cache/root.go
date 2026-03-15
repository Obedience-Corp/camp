package cache

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the cache command family.
var Cmd = &cobra.Command{
	Use:     "cache",
	Short:   "Manage the navigation index cache",
	Long:    "Manage the navigation index cache used for fast project lookups.",
	GroupID: "navigation",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
