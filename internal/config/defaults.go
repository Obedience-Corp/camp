package config

import "time"

// DefaultCampaignPaths returns the default directory structure for campaigns.
func DefaultCampaignPaths() CampaignPaths {
	return CampaignPaths{
		Projects:  "projects/",
		Worktrees: "worktrees/",
		AIDocs:    "ai_docs/",
		Docs:      "docs/",
		Corpus:    "corpus/",
		Festivals: "festivals/",
		Intents:   "intents/",
	}
}

// DefaultNavigationShortcuts returns the default navigation shortcuts for campaigns.
// These shortcuts allow quick navigation to common directories within a campaign.
func DefaultNavigationShortcuts() map[string]ShortcutConfig {
	return map[string]ShortcutConfig{
		"p":  {Path: "projects/", Description: "Jump to projects directory"},
		"w":  {Path: "worktrees/", Description: "Jump to worktrees directory"},
		"f":  {Path: "festivals/", Description: "Jump to festivals directory"},
		"a":  {Path: "ai_docs/", Description: "Jump to AI docs directory"},
		"d":  {Path: "docs/", Description: "Jump to docs directory"},
		"c":  {Path: "corpus/", Description: "Jump to corpus directory"},
		"r":  {Path: "code_reviews/", Description: "Jump to code reviews directory"},
		"pi": {Path: "pipelines/", Description: "Jump to pipelines directory"},
		"i":  {Path: "intents/", Description: "Jump to intents directory"},
	}
}

// DefaultGlobalConfig returns the default global configuration.
func DefaultGlobalConfig() GlobalConfig {
	return GlobalConfig{
		DefaultType:  CampaignTypeProduct,
		Editor:       "", // Uses $EDITOR environment variable
		NoColor:      false,
		Verbose:      false,
		DefaultPaths: DefaultCampaignPaths(),
	}
}

// DefaultCampaignConfig returns a default campaign configuration with the given name.
func DefaultCampaignConfig(name string) CampaignConfig {
	return CampaignConfig{
		Name:      name,
		Type:      CampaignTypeProduct,
		CreatedAt: time.Now(),
		Paths:     DefaultCampaignPaths(),
		Shortcuts: DefaultNavigationShortcuts(),
		Projects:  nil,
	}
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		Campaigns: make(map[string]RegisteredCampaign),
	}
}

// ApplyDefaults fills in missing fields with default values.
func (c *CampaignConfig) ApplyDefaults() {
	if c.Type == "" {
		c.Type = CampaignTypeProduct
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	defaults := DefaultCampaignPaths()
	if c.Paths.Projects == "" {
		c.Paths.Projects = defaults.Projects
	}
	if c.Paths.Worktrees == "" {
		c.Paths.Worktrees = defaults.Worktrees
	}
	if c.Paths.AIDocs == "" {
		c.Paths.AIDocs = defaults.AIDocs
	}
	if c.Paths.Docs == "" {
		c.Paths.Docs = defaults.Docs
	}
	if c.Paths.Corpus == "" {
		c.Paths.Corpus = defaults.Corpus
	}
	if c.Paths.Festivals == "" {
		c.Paths.Festivals = defaults.Festivals
	}
	if c.Paths.Intents == "" {
		c.Paths.Intents = defaults.Intents
	}
}

// ApplyDefaults fills in missing fields with default values.
func (c *GlobalConfig) ApplyDefaults() {
	if c.DefaultType == "" {
		c.DefaultType = CampaignTypeProduct
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
	if c.DefaultPaths.Corpus == "" {
		c.DefaultPaths.Corpus = defaults.Corpus
	}
	if c.DefaultPaths.Festivals == "" {
		c.DefaultPaths.Festivals = defaults.Festivals
	}
	if c.DefaultPaths.Intents == "" {
		c.DefaultPaths.Intents = defaults.Intents
	}
}
