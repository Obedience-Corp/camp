package main

import (
	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage campaign skill directory links",
	Long: `Manage campaign skill directory links for tool interoperability.

Skills are centralized in .campaign/skills/ and exposed to tool ecosystems
(Claude, agents, etc.) via symlinks. This keeps a single source of truth
while making skills discoverable by each tool's native path conventions.

Commands:
  link     Create symlinks from tool-specific paths to .campaign/skills/
  status   Show the current state of skill symlinks
  unlink   Remove skill symlinks

Examples:
  camp skills link claude           Link skills into .claude/skills/
  camp skills link agents           Link skills into .agents/skills/
  camp skills status                Show all skill link states
  camp skills unlink claude         Remove .claude/skills/ symlink`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(skillsCmd)
	skillsCmd.GroupID = "campaign"
}
