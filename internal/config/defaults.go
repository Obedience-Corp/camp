package config

import "time"

// defaultCampaignsDirTilde is the built-in default returned by
// ResolvedCampaignsDir() when GlobalConfig.CampaignsDir is empty.
const defaultCampaignsDirTilde = "~/campaigns/"

// DefaultIntentTags returns the starter tag list for a fresh campaign.
func DefaultIntentTags() []string {
	return []string{"personal", "reference", "question", "follow-up"}
}

// DefaultConcepts returns the default concept configuration for campaigns.
// The picker is a small tree: projects, a single workflow parent whose children
// are the workflow collections (festivals, design, explore, code_reviews,
// pipelines), and docs. workflow keeps its own Path so custom workflow dirs are
// still surfaced from disk.
//
// worktrees, intents, and dungeon are intentionally not picker concepts:
// worktrees is a projects detail, intents would be circular, and dungeon is
// resolved dynamically by nearest-path lookup.
func DefaultConcepts() []ConceptEntry {
	depth0 := 0
	depth1 := 1

	return []ConceptEntry{
		{
			Name:        "projects",
			Path:        "projects/",
			Description: "Active development projects",
			Depth:       &depth1,
			Ignore:      []string{"worktrees/"},
		},
		{
			Name:        "workflow",
			Path:        "workflow/",
			Description: "Workflows",
			Children: []ConceptEntry{
				{Name: "festivals", Path: "festivals/", Description: "Multi-step festival plans"},
				{Name: "design", Path: "workflow/design/", Description: "Design documents", Depth: &depth1},
				{Name: "explore", Path: "workflow/explore/", Description: "Exploratory notes and discovery work", Depth: &depth1},
				{Name: "code_reviews", Path: "workflow/code_reviews/", Description: "Code reviews", Depth: &depth1},
				{Name: "pipelines", Path: "workflow/pipelines/", Description: "Pipelines", Depth: &depth1},
			},
		},
		{
			Name:        "docs",
			Path:        "docs/",
			Description: "Documentation",
			Depth:       &depth0,
		},
	}
}

// DefaultCampaignPaths returns the default directory structure for campaigns.
func DefaultCampaignPaths() CampaignPaths {
	return CampaignPaths{
		Projects:    "projects/",
		Worktrees:   "projects/worktrees/",
		Docs:        "docs/",
		Festivals:   "festivals/",
		Workflow:    "workflow/",
		Intents:     ".campaign/intents/",
		CodeReviews: "workflow/code_reviews/",
		Pipelines:   "workflow/pipelines/",
		Design:      "workflow/design/",
		Dungeon:     "dungeon/",
	}
}

// DefaultNavigationShortcuts returns the default navigation shortcuts for campaigns.
// These shortcuts allow quick navigation to common directories within a campaign.
// Shortcuts with Concept support command expansion (e.g., `camp p commit` -> `camp project commit`).
func DefaultNavigationShortcuts() map[string]ShortcutConfig {
	return map[string]ShortcutConfig{
		// Shortcuts with both navigation and command expansion
		"p":  {Path: "projects/", Concept: "project", Description: "Projects directory and commands", Source: ShortcutSourceAuto},
		"f":  {Path: "festivals/", Concept: "festival", Description: "Festivals directory and commands", Source: ShortcutSourceAuto},
		"i":  {Path: ".campaign/intents/", Concept: "intent", Description: "Intents directory and commands", Source: ShortcutSourceAuto},
		"wt": {Path: "projects/worktrees/", Concept: "project worktree", Description: "Project worktree directory and commands", Source: ShortcutSourceAuto},

		// Navigation-only shortcuts (no command expansion)
		"w":        {Path: "workflow/", Description: "Jump to workflow directory", Source: ShortcutSourceAuto},
		"d":        {Path: "docs/", Description: "Jump to docs directory", Source: ShortcutSourceAuto},
		"du":       {Path: "dungeon/", Description: "Jump to dungeon directory (navigation only)", Source: ShortcutSourceAuto},
		"cr":       {Path: "workflow/code_reviews/", Description: "Jump to code reviews", Source: ShortcutSourceAuto},
		"pi":       {Path: "workflow/pipelines/", Description: "Jump to pipelines", Source: ShortcutSourceAuto},
		"de":       {Path: "workflow/design/", Description: "Jump to design", Source: ShortcutSourceAuto},
		"ex":       {Path: "workflow/explore/", Description: "Jump to explore", Source: ShortcutSourceAuto},
		"settings": {Path: ".campaign/", Description: "Jump to campaign settings directory", Source: ShortcutSourceAuto},

		// Navigation + command expansion
		"cfg": {Path: ".campaign/", Concept: "config", Description: "Campaign config directory and commands", Source: ShortcutSourceAuto},
	}
}

// MergeShortcuts merges user shortcuts with defaults.
// User shortcuts take precedence over defaults.
func MergeShortcuts(user, defaults map[string]ShortcutConfig) map[string]ShortcutConfig {
	result := make(map[string]ShortcutConfig)

	// Start with defaults
	for k, v := range defaults {
		result[k] = v
	}

	// Override with user config
	for k, v := range user {
		result[k] = v
	}

	return result
}

// DefaultTUIConfig returns the default TUI configuration.
func DefaultTUIConfig() TUIConfig {
	return TUIConfig{
		Theme:   "adaptive", // Auto-detect based on terminal
		VimMode: false,
	}
}

// DefaultGlobalConfig returns the default global configuration.
func DefaultGlobalConfig() GlobalConfig {
	return GlobalConfig{
		Editor:  "", // Uses $EDITOR environment variable
		NoColor: false,
		Verbose: false,
		TUI:     DefaultTUIConfig(),
	}
}

// DefaultCampaignConfig returns a default campaign configuration with the given name.
// Note: Paths and shortcuts are now in .campaign/settings/jumps.yaml
func DefaultCampaignConfig(name string) CampaignConfig {
	jumps := DefaultJumpsConfig()
	return CampaignConfig{
		Name:      name,
		Type:      CampaignTypeProduct,
		CreatedAt: time.Now(),
		Projects:  nil,
		Jumps:     &jumps,
	}
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		Campaigns: make(map[string]RegisteredCampaign),
	}
}

// ApplyDefaults fills in missing fields with default values.
// Note: Paths and shortcuts defaults are now applied via JumpsConfig.ApplyDefaults()
func (c *CampaignConfig) ApplyDefaults() {
	if c.Type == "" {
		c.Type = CampaignTypeProduct
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
}

// ApplyDefaults fills in missing fields with default values.
func (c *GlobalConfig) ApplyDefaults() {
	// Apply TUI defaults
	if c.TUI.Theme == "" {
		c.TUI.Theme = ThemeNameAdaptive
	}
}
