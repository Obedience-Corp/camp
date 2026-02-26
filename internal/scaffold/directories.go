// Package scaffold provides campaign initialization and scaffolding functionality.
package scaffold

// DirectoryPurposes maps directories to their purpose descriptions.
// Used for documentation and help text.
var DirectoryPurposes = map[string]string{
	"projects":              "Contains all project repositories and worktrees.",
	"projects/worktrees":    "Git worktrees for parallel development branches.",
	"ai_docs":               "AI-generated documentation and research materials.",
	"docs":                  "Human-authored documentation and specifications.",
	"dungeon":               "Archived, deprioritized, or paused work.",
	"workflow":              "Development workflow artifacts and processes.",
	"workflow/code_reviews": "Code review notes and feedback documents.",
	"workflow/pipelines":    "CI/CD pipeline definitions and automation scripts.",
	"workflow/design":       "Design documents and specifications.",
	"workflow/explore":      "Research, exploration, and investigation workspace.",
	"workflow/intents":      "Future work items not yet ready for Festivals.",
}
