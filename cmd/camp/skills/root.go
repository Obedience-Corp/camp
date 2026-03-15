package skills

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the skills command family.
var Cmd = &cobra.Command{
	Use:     "skills",
	Short:   "Manage campaign skill directory links",
	GroupID: "campaign",
	Long: `Manage campaign skill bundle projection for tool interoperability.

Skills are centralized in .campaign/skills/ and projected into tool ecosystems
(Claude, agents, etc.) as per-bundle symlinks. This keeps a single source of
truth while preserving existing provider-native skills directories.

Commands:
  link     Project per-skill symlinks into a tool-specific skills directory
  status   Show projection status for tool-specific skills directories
  unlink   Remove projected skill symlinks

Examples:
  camp skills link --tool claude    Project skills into .claude/skills/
  camp skills link --tool agents    Project skills into .agents/skills/
  camp skills status                Show all skill projection states
  camp skills unlink --tool claude  Remove projected symlinks from .claude/skills/`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
