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
	// Mission is the campaign's mission statement - the goal or purpose.
	Mission string `yaml:"mission,omitempty"`
	// CreatedAt is when the campaign was created.
	CreatedAt time.Time `yaml:"created_at,omitempty"`
	// Projects contains the list of project configurations.
	Projects []ProjectConfig `yaml:"projects,omitempty"`
	// ConceptList defines concepts for the picker (order is preserved).
	// If empty, concepts are derived from Paths.
	ConceptList []ConceptEntry `yaml:"concepts,omitempty"`

	// Jumps holds the loaded jumps configuration (from .campaign/settings/jumps.yaml).
	// This field is not serialized to campaign.yaml - it's loaded separately.
	Jumps *JumpsConfig `yaml:"-"`
}

// ConceptEntry defines a concept for the picker with ordering and depth control.
type ConceptEntry struct {
	// Name is the concept name (e.g., "projects", "festivals").
	Name string `yaml:"name"`
	// Path is the relative path from campaign root.
	Path string `yaml:"path"`
	// Description provides human-readable help text.
	Description string `yaml:"description,omitempty"`
	// Depth controls drilling depth: nil=unlimited, 0=no drilling, 1+=levels.
	Depth *int `yaml:"depth,omitempty"`
	// Ignore lists subdirectories to exclude from listing.
	Ignore []string `yaml:"ignore,omitempty"`
}

// Paths returns the campaign paths configuration.
// Returns from Jumps if loaded, otherwise returns defaults.
func (c *CampaignConfig) Paths() CampaignPaths {
	if c.Jumps != nil {
		return c.Jumps.Paths
	}
	return DefaultCampaignPaths()
}

// Shortcuts returns the campaign shortcuts configuration.
// Returns from Jumps if loaded, otherwise returns defaults.
func (c *CampaignConfig) Shortcuts() map[string]ShortcutConfig {
	if c.Jumps != nil && c.Jumps.Shortcuts != nil {
		return c.Jumps.Shortcuts
	}
	return DefaultNavigationShortcuts()
}

// Concepts returns the concept list for the picker.
// If ConceptList is defined in campaign.yaml, returns it directly (preserving order).
// Otherwise, derives concepts from CampaignPaths as a fallback.
func (c *CampaignConfig) Concepts() []ConceptEntry {
	if len(c.ConceptList) > 0 {
		return c.ConceptList
	}
	// Fallback: derive from CampaignPaths
	return c.deriveConceptsFromPaths()
}

// deriveConceptsFromPaths creates ConceptEntry list from CampaignPaths.
// This provides backwards compatibility when concepts aren't explicitly defined.
func (c *CampaignConfig) deriveConceptsFromPaths() []ConceptEntry {
	paths := c.Paths()
	defs := []struct {
		name        string
		path        string
		description string
	}{
		{"projects", paths.Projects, "Projects directory"},
		{"worktrees", paths.Worktrees, "Git worktrees"},
		{"festivals", paths.Festivals, "Festivals directory"},
		{"intents", paths.Intents, "Intents directory"},
		{"workflow", paths.Workflow, "Workflow directory"},
		{"code_reviews", paths.CodeReviews, "Code reviews"},
		{"pipelines", paths.Pipelines, "Pipelines"},
		{"design", paths.Design, "Design documents"},
		{"ai_docs", paths.AIDocs, "AI documentation"},
		{"docs", paths.Docs, "Documentation"},
		{"dungeon", paths.Dungeon, "Archived/paused work"},
	}

	var concepts []ConceptEntry
	for _, def := range defs {
		if def.path == "" {
			continue
		}
		concepts = append(concepts, ConceptEntry{
			Name:        def.name,
			Path:        def.path,
			Description: def.description,
		})
	}
	return concepts
}

// CampaignPaths defines the directory structure for a campaign.
type CampaignPaths struct {
	// Projects is the path to the projects directory.
	Projects string `yaml:"projects,omitempty"`
	// Worktrees is the path to git worktrees directory (under projects/).
	Worktrees string `yaml:"worktrees,omitempty"`
	// AIDocs is the path to AI documentation directory.
	AIDocs string `yaml:"ai_docs,omitempty"`
	// Docs is the path to documentation directory.
	Docs string `yaml:"docs,omitempty"`
	// Festivals is the path to festivals directory.
	Festivals string `yaml:"festivals,omitempty"`
	// Workflow is the path to the workflow directory.
	Workflow string `yaml:"workflow,omitempty"`
	// Intents is the path to intents directory (under workflow/).
	Intents string `yaml:"intents,omitempty"`
	// CodeReviews is the path to code reviews directory (under workflow/).
	CodeReviews string `yaml:"code_reviews,omitempty"`
	// Pipelines is the path to pipelines directory (under workflow/).
	Pipelines string `yaml:"pipelines,omitempty"`
	// Design is the path to design directory (under workflow/).
	Design string `yaml:"design,omitempty"`
	// Dungeon is the path to dungeon directory (archived/paused work).
	Dungeon string `yaml:"dungeon,omitempty"`
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
	// Example: "projects/" means `cgo p` navigates to projects directory.
	Path string `yaml:"path,omitempty"`
	// Command is the command to execute for command shortcuts.
	Command string `yaml:"command,omitempty"`
	// WorkDir is the working directory for command execution (relative to campaign root).
	WorkDir string `yaml:"workdir,omitempty"`
	// Description provides help text for this shortcut.
	Description string `yaml:"description,omitempty"`
	// Concept is the command group this shortcut expands to.
	// Example: "project" means `camp p commit` expands to `camp project commit`.
	// If empty, the shortcut does not support command expansion.
	Concept string `yaml:"concept,omitempty"`
}

// IsNavigation returns true if this is a navigation shortcut (has Path).
func (s ShortcutConfig) IsNavigation() bool {
	return s.Path != ""
}

// IsCommand returns true if this is a command shortcut (has Command).
func (s ShortcutConfig) IsCommand() bool {
	return s.Command != ""
}

// HasConcept returns true if this shortcut can be used for command expansion.
func (s ShortcutConfig) HasConcept() bool {
	return s.Concept != ""
}

// HasPath returns true if this shortcut can be used for navigation.
func (s ShortcutConfig) HasPath() bool {
	return s.Path != ""
}

// IsNavigationOnly returns true if shortcut only supports navigation.
func (s ShortcutConfig) IsNavigationOnly() bool {
	return s.HasPath() && !s.HasConcept()
}

// IsConceptOnly returns true if shortcut only supports command expansion.
func (s ShortcutConfig) IsConceptOnly() bool {
	return s.HasConcept() && !s.HasPath()
}

// TUIConfig holds configuration for terminal UI elements.
type TUIConfig struct {
	// Theme is the color theme for huh forms (adaptive, light, dark, high-contrast).
	Theme string `json:"theme,omitempty" yaml:"theme,omitempty"`
	// VimMode enables vim-style keybindings in forms.
	VimMode bool `json:"vim_mode,omitempty" yaml:"vim_mode,omitempty"`
}

// GlobalConfig represents ~/.config/obey/campaign/config.json configuration.
// Contains only user preference fields - campaign-specific settings belong elsewhere.
type GlobalConfig struct {
	// Editor is the preferred editor command.
	Editor string `json:"editor,omitempty" yaml:"editor,omitempty"`
	// NoColor disables colored output.
	NoColor bool `json:"no_color,omitempty" yaml:"no_color,omitempty"`
	// Verbose enables verbose output.
	Verbose bool `json:"verbose,omitempty" yaml:"verbose,omitempty"`
	// TUI holds terminal UI configuration.
	TUI TUIConfig `json:"tui,omitempty" yaml:"tui,omitempty"`
}

// RegistryVersion is the current registry format version.
const RegistryVersion = 2

// Registry represents ~/.config/obey/campaign/registry.json for tracking campaigns.
type Registry struct {
	// Version is the registry format version.
	Version int `json:"version" yaml:"version,omitempty"`
	// Campaigns maps campaign IDs to their registration info.
	Campaigns map[string]RegisteredCampaign `json:"campaigns" yaml:"campaigns,omitempty"`

	// pathIndex maps paths to campaign IDs for fast lookup (not serialized).
	pathIndex map[string]string `json:"-" yaml:"-"`
}

// RegisteredCampaign holds information about a registered campaign.
type RegisteredCampaign struct {
	// ID is the unique campaign identifier (UUID v4).
	// In JSON format, the ID is the map key, so this field is only used for YAML compatibility.
	ID string `json:"-" yaml:"id"`
	// Name is the campaign name (for display and lookup).
	Name string `json:"name" yaml:"name"`
	// Path is the absolute path to the campaign root.
	Path string `json:"path" yaml:"path"`
	// Type is the campaign type.
	Type CampaignType `json:"type,omitempty" yaml:"type,omitempty"`
	// LastAccess is when the campaign was last accessed.
	LastAccess time.Time `json:"last_access,omitempty" yaml:"last_access,omitempty"`
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

// VerificationReport contains results from registry verification.
type VerificationReport struct {
	Removed       []RemovedEntry
	Updated       []UpdatedEntry
	Added         []AddedEntry
	TotalVerified int
}

// HasChanges returns true if any changes were made during verification.
func (r *VerificationReport) HasChanges() bool {
	return len(r.Removed) > 0 || len(r.Updated) > 0 || len(r.Added) > 0
}

// RemovedEntry represents a registry entry that was removed during verification.
type RemovedEntry struct {
	ID     string
	Name   string
	Path   string
	Reason string
}

// UpdatedEntry represents a registry entry that was updated during verification.
type UpdatedEntry struct {
	ID      string
	Path    string
	Changes []string
}

// AddedEntry represents a registry entry that was added during verification.
type AddedEntry struct {
	ID   string
	Name string
	Path string
}
