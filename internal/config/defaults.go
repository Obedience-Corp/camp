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
}
