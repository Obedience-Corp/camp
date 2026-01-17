// Package index provides navigation target indexing for campaigns.
package index

import (
	"time"

	"github.com/obediencecorp/camp/internal/nav"
)

// Target represents a navigation target in the index.
type Target struct {
	// Name is the display name of the target.
	Name string `yaml:"name"`
	// Path is the absolute path to the target.
	Path string `yaml:"path"`
	// Category is the category this target belongs to.
	Category nav.Category `yaml:"category"`
	// LastAccess tracks when this target was last navigated to.
	LastAccess time.Time `yaml:"last_access,omitempty"`
}

// Index holds all navigation targets for a campaign.
type Index struct {
	// Targets is the list of all navigation targets.
	Targets []Target `yaml:"targets"`
	// BuildTime is when this index was built.
	BuildTime time.Time `yaml:"build_time"`
	// CampaignRoot is the campaign root this index was built for.
	CampaignRoot string `yaml:"campaign_root"`
	// Version is the index format version.
	Version int `yaml:"version"`
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
