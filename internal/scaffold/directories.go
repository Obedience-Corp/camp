// Package scaffold provides campaign initialization and scaffolding functionality.
package scaffold

// StandardDirs contains all directories created for a full campaign.
var StandardDirs = []string{
	".campaign",
	"projects",
	"worktrees",
	"ai_docs",
	"docs",
	"corpus",
	"pipelines",
	"code_reviews",
	"intents",
}

// MinimalDirs contains the minimum directories for a campaign.
var MinimalDirs = []string{
	".campaign",
	"projects",
	"intents",
}

// CampaignSubdirs contains subdirectories within .campaign/
var CampaignSubdirs = []string{
	"templates",
	"agents",
}

// IntentsSubdirs contains subdirectories within intents/ for full campaigns.
var IntentsSubdirs = []string{
	"inbox",
	"active",
	"ready",
	"done",
	"killed",
}

// IntentsMinimalSubdirs contains subdirectories within intents/ for minimal campaigns.
var IntentsMinimalSubdirs = []string{
	"inbox",
}

// DirectoryPurposes maps directories to their purpose descriptions.
var DirectoryPurposes = map[string]string{
	"projects":     "Contains all project repositories, either as submodules or worktrees.",
	"worktrees":    "Git worktrees for parallel development branches.",
	"ai_docs":      "AI-generated documentation and research materials.",
	"docs":         "Human-authored documentation and specifications.",
	"corpus":       "Reference materials, examples, and knowledge base documents.",
	"pipelines":    "CI/CD pipeline definitions and automation scripts.",
	"code_reviews": "Code review notes and feedback documents.",
	"intents":      "Future work items not yet ready for Festivals.",
}
