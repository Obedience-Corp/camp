package tag

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:     "tag",
	Short:   "Label campaigns with tags",
	GroupID: "registry",
	Long: `Label campaigns with tags from a single global pool.

Tags are orthogonal to orgs: any campaign can carry any tag regardless of its
org, and the same tag can appear across orgs. Tags are a set per campaign
(re-adding is a no-op).

Commands:
  add   Add tags to a campaign
  rm    Remove tags from a campaign
  list  List all tags in use with counts`,
	Example: `  camp tag add obey-campaign paid-work q3-2026
  camp tag rm obey-campaign q3-2026
  camp tag list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
