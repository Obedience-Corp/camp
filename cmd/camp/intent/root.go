package intent

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the idea/intent command family. "idea" is the
// primary, user-facing name; "intent" (the original name) and "ideas" remain
// working aliases indefinitely — the on-disk storage path stays
// .campaign/intents/ regardless of which name is used to invoke it.
var Cmd = &cobra.Command{
	Use:     "idea",
	Aliases: []string{"intent", "ideas"},
	Short:   "Manage campaign ideas",
	GroupID: "planning",
	Long: `Manage ideas for features and improvements not yet ready for full planning.

Ideas capture thoughts, bugs, features, and research topics that depend on work
not yet completed. They serve as structured storage for ideas that aren't ready
to become Festivals but need to be tracked.

CAPTURE MODES:
  Fast (default)    Quick capture with minimal fields
  Deep (--edit)     Open in editor for full context

IDEA LIFECYCLE:
  inbox  → Captured, not yet reviewed
  ready  → Reviewed/enriched, ready for promotion
  active → Promoted to festival/design doc, work in progress
  dungeon/* → Terminal statuses (done, killed, archived, someday)

"camp intent" (the original name) keeps working as an alias for every
command below; the storage path is .campaign/intents/ either way.

Examples:
  camp idea add "Add dark mode toggle"         Fast capture to inbox
  camp idea add -e "Refactor auth system"      Deep capture with editor
  camp idea list                               List all ideas
  camp idea list --status active               List active ideas
  camp idea edit add-dark                      Edit idea (fuzzy match)
  camp idea show 20260119-153412-add-dark      Show idea details
  camp idea move add-dark ready                Mark as ready
  camp idea promote add-dark                   Promote to active via festival
  camp idea archive add-dark                   Archive idea`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
