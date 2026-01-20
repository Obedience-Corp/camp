package main

import (
	"github.com/spf13/cobra"
)

var intentCmd = &cobra.Command{
	Use:   "intent",
	Short: "Manage campaign intents",
	Long: `Manage intents for ideas and features not yet ready for full planning.

Intents capture ideas, bugs, features, and research topics that depend on work
not yet completed. They serve as structured storage for ideas that aren't ready
to become Festivals but need to be tracked.

CAPTURE MODES:
  Fast (default)    Quick capture with minimal fields
  Deep (--edit)     Open in editor for full context

INTENT LIFECYCLE:
  inbox  → Captured, not yet reviewed
  active → Being enriched with details
  ready  → Ready for Festival promotion
  done   → Resolved
  killed → Abandoned

Examples:
  camp intent add "Add dark mode toggle"         Fast capture to inbox
  camp intent add -e "Refactor auth system"      Deep capture with editor
  camp intent list                               List all intents
  camp intent list --status active               List active intents
  camp intent edit add-dark                      Edit intent (fuzzy match)
  camp intent show 20260119-153412-add-dark      Show intent details
  camp intent move add-dark active               Move to active status
  camp intent promote add-dark                   Promote to Festival
  camp intent archive add-dark                   Archive intent`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(intentCmd)
	intentCmd.GroupID = "planning"
}
