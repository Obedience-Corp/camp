// Package config provides configuration types and loading for camp.
package config

import "time"

// CampaignType represents the type of campaign.
type CampaignType string

const (
	// CampaignTypeProduct is for product development campaigns.
	CampaignTypeProduct CampaignType = "product"
	// CampaignTypeResearch is for research-focused campaigns.
	CampaignTypeResearch CampaignType = "research"
	// CampaignTypeTools is for tooling and infrastructure campaigns.
	CampaignTypeTools CampaignType = "tools"
	// CampaignTypePersonal is for personal projects.
	CampaignTypePersonal CampaignType = "personal"
)

// CampaignConfig represents .campaign/campaign.yaml configuration.
type CampaignConfig struct {
	// Name is the campaign name.
	Name string `yaml:"name"`
	// Type is the campaign type.
	Type CampaignType `yaml:"type"`
	// Description is a brief description of the campaign.
	Description string `yaml:"description,omitempty"`
	// CreatedAt is when the campaign was created.
	CreatedAt time.Time `yaml:"created_at,omitempty"`
	// Paths defines the campaign directory structure.
	Paths CampaignPaths `yaml:"paths,omitempty"`
	// Projects contains the list of project configurations.
	Projects []ProjectConfig `yaml:"projects,omitempty"`
}

// CampaignPaths defines the directory structure for a campaign.
type CampaignPaths struct {
	// Projects is the path to the projects directory.
	Projects string `yaml:"projects,omitempty"`
	// Worktrees is the path to git worktrees directory.
	Worktrees string `yaml:"worktrees,omitempty"`
	// AIDocs is the path to AI documentation directory.
	AIDocs string `yaml:"ai_docs,omitempty"`
	// Docs is the path to documentation directory.
	Docs string `yaml:"docs,omitempty"`
	// Corpus is the path to corpus/reference material directory.
	Corpus string `yaml:"corpus,omitempty"`
	// Festivals is the path to festivals directory.
	Festivals string `yaml:"festivals,omitempty"`
}

// ProjectConfig holds configuration for a single project in the campaign.
type ProjectConfig struct {
	// Name is the project name (directory name).
	Name string `yaml:"name"`
	// Path is the relative path to the project.
	Path string `yaml:"path"`
	// URL is the git remote URL (for submodules).
	URL string `yaml:"url,omitempty"`
	// Branch is the default branch for the project.
	Branch string `yaml:"branch,omitempty"`
}

// GlobalConfig represents ~/.config/campaign/config.yaml configuration.
type GlobalConfig struct {
	// DefaultType is the default campaign type when creating new campaigns.
	DefaultType CampaignType `yaml:"default_type,omitempty"`
	// Editor is the preferred editor command.
	Editor string `yaml:"editor,omitempty"`
	// NoColor disables colored output.
	NoColor bool `yaml:"no_color,omitempty"`
	// Verbose enables verbose output.
	Verbose bool `yaml:"verbose,omitempty"`
	// DefaultPaths provides default paths for new campaigns.
	DefaultPaths CampaignPaths `yaml:"default_paths,omitempty"`
}

// Registry represents ~/.config/campaign/registry.yaml for tracking campaigns.
type Registry struct {
	// Campaigns maps campaign names to their registration info.
	Campaigns map[string]RegisteredCampaign `yaml:"campaigns,omitempty"`
}

// RegisteredCampaign holds information about a registered campaign.
type RegisteredCampaign struct {
	// Path is the absolute path to the campaign root.
	Path string `yaml:"path"`
	// Type is the campaign type.
	Type CampaignType `yaml:"type,omitempty"`
	// LastAccess is when the campaign was last accessed.
	LastAccess time.Time `yaml:"last_access,omitempty"`
}

// Valid returns true if the campaign type is a known valid type.
func (t CampaignType) Valid() bool {
	switch t {
	case CampaignTypeProduct, CampaignTypeResearch, CampaignTypeTools, CampaignTypePersonal:
		return true
	default:
		return false
	}
}

// String returns the string representation of the campaign type.
func (t CampaignType) String() string {
	return string(t)
}
