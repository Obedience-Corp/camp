package refs

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the refs-sync command owned by the refs package.
var Cmd = &cobra.Command{
	Use:   "refs-sync [submodule...]",
	Short: "Sync submodule ref pointers in campaign root",
	Long: `Update the campaign root's recorded submodule pointers to match
each submodule's current HEAD. Creates a single atomic commit.

Without arguments, syncs all submodules. Specify paths to sync specific ones.

Examples:
  camp refs-sync                      # Sync all dirty refs
  camp refs-sync projects/camp        # Sync specific submodule
  camp refs-sync --dry-run            # Show plan without executing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
