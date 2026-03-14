package dungeon

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the dungeon command family.
var Cmd = &cobra.Command{
	Use:     "dungeon",
	Short:   "Manage the campaign dungeon",
	GroupID: "planning",
	Long: `Manage the campaign dungeon - a holding area for uncertain work.

The dungeon is where you put work you're unsure about or want out of the way.
It keeps items visible without them competing for your attention.

Commands:
  add     Initialize dungeon structure with documentation
  crawl   Interactive review and archival of dungeon contents
  list    List dungeon items (agent-friendly)
  move    Move items between dungeon statuses (agent-friendly)

Examples:
  camp dungeon add                        Initialize the dungeon
  camp dungeon crawl                      Review and archive dungeon items
  camp dungeon list                       List dungeon root items
  camp dungeon list --triage              List parent items eligible for triage
  camp dungeon move old-feature archived  Move item to archived status`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
