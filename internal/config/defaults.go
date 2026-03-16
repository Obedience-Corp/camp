package config

import "time"

// DefaultConcepts returns the default concept configuration for campaigns.
// Each concept has a name, path, description, and optional depth/ignore settings.
func DefaultConcepts() []ConceptEntry {
	depth0 := 0
	depth1 := 1

	// Dungeon is intentionally not modeled as a default concept entry.
	// Dungeon context is resolved dynamically by nearest-path lookup, and a
	// static concept path implies root-only behavior that conflicts with that model.
	return []ConceptEntry{
		{
			Name:        "projects",
			Path:        "projects/",
			Description: "Active development projects",
			Depth:       &depth1,
			Ignore:      []string{"worktrees/"},
		},
		{
			Name:        "worktrees",
			Path:        "projects/worktrees/",
			Description: "Git worktrees",
			Depth:       &depth1,
		},
		{
			Name:        "festivals",
			Path:        "festivals/",
			Description: "Planning cycles",
			// Depth nil = unlimited
		},
		{
			Name:        "intents",
			Path:        "workflow/intents/",
			Description: "Ideas and tasks",
			Depth:       &depth0,
		},
		{
			Name:        "workflow",
			Path:        "workflow/",
			Description: "Work management",
			Depth:       &depth1,
			Ignore:      []string{"intents/", "design/", "explore/"},
		},
		{
			Name:        "design",
			Path:        "workflow/design/",
			Description: "Design documents",
			Depth:       &depth1,
		},
		{
			Name:        "explore",
			Path:        "workflow/explore/",
			Description: "Exploratory notes and discovery work",
			Depth:       &depth1,
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
		AIDocs:      "ai_docs/",
		Docs:        "docs/",
		Festivals:   "festivals/",
		Workflow:    "workflow/",
		Intents:     "workflow/intents/",
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
		"i":  {Path: "workflow/intents/", Concept: "intent", Description: "Intents directory and commands", Source: ShortcutSourceAuto},
		"wt": {Path: "projects/worktrees/", Concept: "worktrees", Description: "Worktrees directory and commands", Source: ShortcutSourceAuto},

		// Navigation-only shortcuts (no command expansion)
		"w":  {Path: "workflow/", Description: "Jump to workflow directory", Source: ShortcutSourceAuto},
		"ai": {Path: "ai_docs/", Description: "Jump to AI docs directory", Source: ShortcutSourceAuto},
		"d":  {Path: "docs/", Description: "Jump to docs directory", Source: ShortcutSourceAuto},
		"du": {Path: "dungeon/", Description: "Jump to dungeon directory (navigation only)", Source: ShortcutSourceAuto},
		"cr": {Path: "workflow/code_reviews/", Description: "Jump to code reviews", Source: ShortcutSourceAuto},
		"pi": {Path: "workflow/pipelines/", Description: "Jump to pipelines", Source: ShortcutSourceAuto},
		"de": {Path: "workflow/design/", Description: "Jump to design", Source: ShortcutSourceAuto},
		"ex": {Path: "workflow/explore/", Description: "Jump to explore", Source: ShortcutSourceAuto},

		// Command-only shortcuts (no navigation path)
		"cfg": {Concept: "config", Description: "Config commands", Source: ShortcutSourceAuto},
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
		c.TUI.Theme = "adaptive"
	}
}
