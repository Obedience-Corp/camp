// Package index provides navigation target indexing for campaigns.
package index

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/obediencecorp/camp/internal/nav"
)

// Target represents a navigation target in the index.
type Target struct {
	// Name is the display name of the target.
	Name string `json:"name"`
	// Path is the absolute path to the target.
	Path string `json:"path"`
	// Category is the category this target belongs to.
	Category nav.Category `json:"category"`
	// LastAccess tracks when this target was last navigated to.
	LastAccess time.Time `json:"last_access,omitempty"`
	// Shortcuts maps shortcut names to relative paths within the target.
	// Used for project sub-shortcuts (e.g., "cli" -> "fest/cmd/fest/").
	Shortcuts map[string]string `json:"shortcuts,omitempty"`
}

// JumpPath returns the absolute path to jump to, optionally using a sub-shortcut.
// If subShortcut is provided and exists, returns path + shortcut path.
// If subShortcut is empty and "default" shortcut exists, uses that.
// Otherwise returns the target's base path.
func (t *Target) JumpPath(subShortcut string) string {
	if t.Shortcuts == nil {
		return t.Path
	}

	// Try explicit sub-shortcut first
	if subShortcut != "" {
		if subPath, ok := t.Shortcuts[subShortcut]; ok {
			return filepath.Join(t.Path, subPath)
		}
		// Invalid shortcut - return empty to signal error
		return ""
	}

	// Fall back to default shortcut if it exists
	if defaultPath, ok := t.Shortcuts["default"]; ok {
		return filepath.Join(t.Path, defaultPath)
	}

	return t.Path
}

// HasShortcut returns true if the target has the named shortcut.
func (t *Target) HasShortcut(name string) bool {
	if t.Shortcuts == nil {
		return false
	}
	_, ok := t.Shortcuts[name]
	return ok
}

// HasShortcuts returns true if the target has any shortcuts defined.
func (t *Target) HasShortcuts() bool {
	return len(t.Shortcuts) > 0
}

// ShortcutNames returns sorted list of shortcut names for this target.
func (t *Target) ShortcutNames() []string {
	if t.Shortcuts == nil {
		return nil
	}
	names := make([]string, 0, len(t.Shortcuts))
	for name := range t.Shortcuts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Index holds all navigation targets for a campaign.
type Index struct {
	// Targets is the list of all navigation targets.
	Targets []Target `json:"targets"`
	// BuildTime is when this index was built.
	BuildTime time.Time `json:"build_time"`
	// CampaignRoot is the campaign root this index was built for.
	CampaignRoot string `json:"campaign_root"`
	// Version is the index format version.
	Version int `json:"version"`
}

// IndexVersion is the current index format version.
const IndexVersion = 1

// NewIndex creates a new empty index for a campaign root.
func NewIndex(campaignRoot string) *Index {
	return &Index{
		Targets:      make([]Target, 0),
		BuildTime:    time.Now(),
		CampaignRoot: campaignRoot,
		Version:      IndexVersion,
	}
}

// Len returns the number of targets in the index.
func (idx *Index) Len() int {
	return len(idx.Targets)
}

// ByCategory returns targets filtered by category.
func (idx *Index) ByCategory(cat nav.Category) []Target {
	if cat == nav.CategoryAll {
		return idx.Targets
	}

	result := make([]Target, 0)
	for _, t := range idx.Targets {
		if t.Category == cat {
			result = append(result, t)
		}
	}
	return result
}

// Names returns the names of all targets.
func (idx *Index) Names() []string {
	names := make([]string, len(idx.Targets))
	for i, t := range idx.Targets {
		names[i] = t.Name
	}
	return names
}

// Find returns the first target matching the given name.
// Returns nil if not found.
func (idx *Index) Find(name string) *Target {
	for i := range idx.Targets {
		if idx.Targets[i].Name == name {
			return &idx.Targets[i]
		}
	}
	return nil
}

// AddTarget adds a target to the index.
func (idx *Index) AddTarget(t Target) {
	idx.Targets = append(idx.Targets, t)
}
