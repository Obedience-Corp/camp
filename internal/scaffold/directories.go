// Package scaffold provides campaign initialization and scaffolding functionality.
package scaffold

// StandardDirs contains all directories created for a full campaign.
// Note: festivals/ is NOT included here - it's created via `fest init`.
var StandardDirs = []string{
	".campaign",
	"projects",
	"ai_docs",
	"docs",
	"dungeon",
	"workflow",
}

// CampaignSubdirs contains subdirectories within .campaign/
var CampaignSubdirs = []string{
	"templates",
	"agents",
}

// ProjectsSubdirs contains subdirectories within projects/
var ProjectsSubdirs = []string{
	"worktrees",
}

// DungeonSubdirs contains subdirectories within dungeon/
var DungeonSubdirs = []string{
	"archived",
}

// WorkflowSubdirs contains subdirectories within workflow/
var WorkflowSubdirs = []string{
	"code_reviews",
	"pipelines",
	"design",
	"intents",
}

// IntentsSubdirs contains subdirectories within workflow/intents/
var IntentsSubdirs = []string{
	"inbox",
	"active",
	"ready",
	"done",
	"killed",
}

// DirectoryPurposes maps directories to their purpose descriptions.
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
	"workflow/intents":      "Future work items not yet ready for Festivals.",
}
