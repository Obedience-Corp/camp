package org

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:     "org",
	Short:   "Group campaigns into orgs",
	GroupID: "registry",
	Long: `Group related campaigns into orgs.

Every campaign belongs to exactly one org (default "default"). Orgs are derived:
an org exists because a campaign names it, and disappears when its last member
leaves. There is no "org create".

Commands:
  add     Assign campaigns to an org (also reassigns; single-membership)
  remove  Return campaigns to the default org`,
	Example: `  camp org                                       Print the current campaign's org
  camp org add obey obey-campaign obey-content   Move campaigns into "obey"
  camp org remove obey-content                   Return a campaign to "default"`,
	Args: cobra.NoArgs,
	RunE: runOrgBare,
}
