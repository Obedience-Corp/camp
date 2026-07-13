// Package scaffold provides campaign initialization and scaffolding functionality.
package scaffold

// DirectoryPurposes maps directories to their purpose descriptions.
// Used for documentation and help text.
var DirectoryPurposes = map[string]string{
	"projects":           "Contains all project repositories and worktrees.",
	"projects/worktrees": "Git worktrees for parallel development branches.",
	"docs":               "Human-authored documentation and specifications.",
	"dungeon":            "Archived, deprioritized, or paused work.",
	"workflow":           "Workflow artifacts and processes.",
	"workflow/reviews":   "Review notes, feedback, and quality documents.",
	"workflow/design":    "Design documents and specifications.",
	"workflow/explore":   "Research, exploration, and investigation workspace.",
	".campaign/intents":  "System-managed intent state used through camp intent.",
}
