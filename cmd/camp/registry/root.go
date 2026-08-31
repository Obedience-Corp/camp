package registry

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the registry command family.
var Cmd = &cobra.Command{
	Use:     "registry",
	Short:   "Manage the camp registry",
	GroupID: "registry",
	Aliases: []string{"reg"},
	Long: `Manage the camp registry at ~/.obey/campaign/registry.json.

The registry tracks all known camps for quick navigation and lookup.
Use these commands to maintain registry health and resolve issues.

Commands:
  prune   Remove stale entries (camps that no longer exist)
  sync    Update registry entry for current camp
  check   Validate registry integrity`,
	Example: `  camp registry prune             Remove entries for non-existent camps
  camp registry prune --dry-run   Show what would be removed
  camp registry sync              Update path for current camp
  camp registry check             Check for issues`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
