package org

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:     "org",
	Short:   "Group campaigns into orgs",
	GroupID: "registry",
	Long: `Group related campaigns into orgs.

Every campaign belongs to exactly one org (default "default"). Orgs are derived:
an org exists because a campaign names it, and disappears when its last member
leaves.

In a terminal, 'camp org' (no arguments) opens an interactive browser of orgs
and their members where you can move, create, rename, and return campaigns. When
piped or with --json it prints the current campaign's org instead; use
'camp org which' to print the org unconditionally.

Commands:
  which   Print the current campaign's org
  create  Create an org by joining campaigns (the current campaign if none named)
  add     Assign campaigns to an org (also reassigns; single-membership)
  remove  Return campaigns to the default org`,
	Example: `  camp org                                       Browse and manage orgs interactively (TTY)
  camp org which                                 Print the current campaign's org
  camp org create obey                           Add the current campaign to "obey"
  camp org add obey obey-campaign obey-content   Move campaigns into "obey"
  camp org remove obey-content                   Return a campaign to "default"`,
	Args: cobra.NoArgs,
	RunE: runOrgBare,
}
