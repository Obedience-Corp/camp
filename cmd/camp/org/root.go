package org

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:     "org",
	Short:   "Group campaigns into orgs",
	GroupID: "registry",
	Long: `Group related campaigns into first-class orgs.

Every campaign belongs to exactly one org (default "default"). Orgs are first-class:
they persist in the machine-wide registry, can hold zero members, and are deleted
explicitly with 'camp org delete'.

In a terminal, 'camp org' (no arguments) opens an interactive browser of orgs
and their members where you can move, create, rename, and return campaigns. When
piped or with --json it prints the current campaign's org instead; use
'camp org which' to print the org unconditionally.

Commands:
  which   Print the current campaign's org
  create  Create an org (optionally --empty) and optionally join campaigns
  add     Assign campaigns to an org (also reassigns; single-membership)
  remove  Return campaigns to the default org
  delete  Delete an org (empty only unless --force)`,
	Example: `  camp org                                       Browse and manage orgs interactively (TTY)
  camp org which                                 Print the current campaign's org
  camp org create obey                           Add the current campaign to "obey"
  camp org create empty-org --empty              Create an org with no members
  camp org add obey obey-campaign obey-content   Move campaigns into "obey"
  camp org remove obey-content                   Return a campaign to "default"
  camp org delete empty-org                      Delete an empty org`,
	Args: cobra.NoArgs,
	RunE: runOrgBare,
}
