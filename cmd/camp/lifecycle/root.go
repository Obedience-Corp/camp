package lifecycle

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:     "lifecycle",
	Short:   "Manage campaign lifecycle status",
	GroupID: "registry",
	Long: `Manage a campaign's lifecycle status.

The status is one of a fixed set:
  active      in current use (default); shown in 'camp list'
  inactive    paused or shelved; hidden from default 'camp list'
  reference   preserved read-only context; hidden from default views

Setting inactive or reference does not unregister the campaign; use
'camp unregister' to remove it from the registry entirely.

This group is 'camp lifecycle', not 'camp status' ('camp status' is the git
status wrapper).`,
	Example: `  camp lifecycle set old-project reference
  camp lifecycle list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
