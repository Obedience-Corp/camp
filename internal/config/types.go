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
	// ID is the unique campaign identifier (UUID v4).
	ID string `yaml:"id"`
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
	// Shortcuts defines custom navigation and command shortcuts.
	Shortcuts map[string]ShortcutConfig `yaml:"shortcuts,omitempty"`
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
	// Intents is the path to intents directory.
	Intents string `yaml:"intents,omitempty"`
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
	// Shortcuts maps shortcut names to relative paths within the project.
	// The "default" key is used when jumping to the project without a sub-shortcut.
	Shortcuts map[string]string `yaml:"shortcuts,omitempty"`
}

// ResolveShortcut returns the relative path for a shortcut name.
// If name is empty, returns the "default" shortcut path.
// Returns empty string if the shortcut doesn't exist.
func (p *ProjectConfig) ResolveShortcut(name string) string {
	if p.Shortcuts == nil {
		return ""
	}
	if name == "" {
		return p.Shortcuts["default"]
	}
	return p.Shortcuts[name]
}

// HasShortcuts returns true if the project has any shortcuts defined.
func (p *ProjectConfig) HasShortcuts() bool {
	return len(p.Shortcuts) > 0
}

// ShortcutNames returns sorted list of shortcut names for this project.
func (p *ProjectConfig) ShortcutNames() []string {
	if p.Shortcuts == nil {
		return nil
	}
	names := make([]string, 0, len(p.Shortcuts))
	for name := range p.Shortcuts {
		names = append(names, name)
	}
	return names
}

// ShortcutConfig defines a custom navigation or command shortcut.
type ShortcutConfig struct {
	// Path is the relative path for navigation shortcuts.
	Path string `yaml:"path,omitempty"`
	// Command is the command to execute for command shortcuts.
	Command string `yaml:"command,omitempty"`
	// WorkDir is the working directory for command execution (relative to campaign root).
	WorkDir string `yaml:"workdir,omitempty"`
	// Description provides help text for this shortcut.
	Description string `yaml:"description,omitempty"`
}

// IsNavigation returns true if this is a navigation shortcut (has Path).
func (s ShortcutConfig) IsNavigation() bool {
	return s.Path != ""
}

// IsCommand returns true if this is a command shortcut (has Command).
func (s ShortcutConfig) IsCommand() bool {
	return s.Command != ""
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
	// Campaigns maps campaign IDs to their registration info.
	Campaigns map[string]RegisteredCampaign `yaml:"campaigns,omitempty"`
}

// RegisteredCampaign holds information about a registered campaign.
type RegisteredCampaign struct {
	// ID is the unique campaign identifier (UUID v4).
	ID string `yaml:"id"`
	// Name is the campaign name (for display and lookup).
	Name string `yaml:"name"`
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
