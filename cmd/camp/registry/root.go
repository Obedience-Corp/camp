package registry

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the registry command family.
var Cmd = &cobra.Command{
	Use:     "registry",
	Short:   "Manage the campaign registry",
	GroupID: "registry",
	Aliases: []string{"reg"},
	Long: `Manage the campaign registry at ~/.obey/campaign/registry.json.

The registry tracks all known campaigns for quick navigation and lookup.
Use these commands to maintain registry health and resolve issues.

Commands:
  prune   Remove stale entries (campaigns that no longer exist)
  sync    Update registry entry for current campaign
  check   Validate registry integrity

Examples:
  camp registry prune             Remove entries for non-existent campaigns
  camp registry prune --dry-run   Show what would be removed
  camp registry sync              Update path for current campaign
  camp registry check             Check for issues`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
