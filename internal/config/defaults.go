package config

import "time"

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
func DefaultNavigationShortcuts() map[string]ShortcutConfig {
	return map[string]ShortcutConfig{
		"p":  {Path: "projects/", Description: "Jump to projects directory"},
		"pw": {Path: "projects/worktrees/", Description: "Jump to project worktrees"},
		"w":  {Path: "workflow/", Description: "Jump to workflow directory"},
		"f":  {Path: "festivals/", Description: "Jump to festivals directory"},
		"a":  {Path: "ai_docs/", Description: "Jump to AI docs directory"},
		"d":  {Path: "docs/", Description: "Jump to docs directory"},
		"du": {Path: "dungeon/", Description: "Jump to dungeon directory"},
		"cr": {Path: "workflow/code_reviews/", Description: "Jump to code reviews"},
		"pi": {Path: "workflow/pipelines/", Description: "Jump to pipelines"},
		"de": {Path: "workflow/design/", Description: "Jump to design"},
		"i":  {Path: "workflow/intents/", Description: "Jump to intents"},
	}
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
		DefaultType:  CampaignTypeProduct,
		Editor:       "", // Uses $EDITOR environment variable
		NoColor:      false,
		Verbose:      false,
		TUI:          DefaultTUIConfig(),
		DefaultPaths: DefaultCampaignPaths(),
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
	if c.DefaultType == "" {
		c.DefaultType = CampaignTypeProduct
	}
	// Apply TUI defaults
	if c.TUI.Theme == "" {
		c.TUI.Theme = "adaptive"
	}
	defaults := DefaultCampaignPaths()
	if c.DefaultPaths.Projects == "" {
		c.DefaultPaths.Projects = defaults.Projects
	}
	if c.DefaultPaths.Worktrees == "" {
		c.DefaultPaths.Worktrees = defaults.Worktrees
	}
	if c.DefaultPaths.AIDocs == "" {
		c.DefaultPaths.AIDocs = defaults.AIDocs
	}
	if c.DefaultPaths.Docs == "" {
		c.DefaultPaths.Docs = defaults.Docs
	}
	if c.DefaultPaths.Festivals == "" {
		c.DefaultPaths.Festivals = defaults.Festivals
	}
	if c.DefaultPaths.Workflow == "" {
		c.DefaultPaths.Workflow = defaults.Workflow
	}
	if c.DefaultPaths.Intents == "" {
		c.DefaultPaths.Intents = defaults.Intents
	}
	if c.DefaultPaths.CodeReviews == "" {
		c.DefaultPaths.CodeReviews = defaults.CodeReviews
	}
	if c.DefaultPaths.Pipelines == "" {
		c.DefaultPaths.Pipelines = defaults.Pipelines
	}
	if c.DefaultPaths.Design == "" {
		c.DefaultPaths.Design = defaults.Design
	}
	if c.DefaultPaths.Dungeon == "" {
		c.DefaultPaths.Dungeon = defaults.Dungeon
	}
}
