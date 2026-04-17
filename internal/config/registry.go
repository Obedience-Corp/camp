package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/config/registryfile"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// ErrMultipleMatches is returned when an ID prefix matches multiple campaigns.
var ErrMultipleMatches = errors.New("multiple campaigns match that prefix")

// ErrCampaignNotFound is returned when a campaign cannot be found.
var ErrCampaignNotFound = errors.New("campaign not found")

// LoadRegistry loads the campaign registry from ~/.obey/campaign/registry.json.
// Returns an empty registry if the file doesn't exist.
func LoadRegistry(ctx context.Context) (*Registry, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	path := RegistryPath()
	file, err := registryfile.Load()
	if err != nil {
		return nil, camperrors.Wrapf(err, "failed to read registry %s", path)
	}

	reg := NewRegistry()
	reg.Version = file.Version
	for id, entry := range file.Campaigns {
		reg.Campaigns[id] = RegisteredCampaign{
			ID:         id,
			Name:       entry.Name,
			Path:       entry.Path,
			Type:       CampaignType(entry.Type),
			LastAccess: entry.LastAccess,
		}
	}

	// Default to current version when loading legacy/empty files.
	if reg.Version == 0 {
		reg.Version = RegistryVersion
	}

	reg.rebuildPathIndex()

	return reg, nil
}

// SaveRegistry saves the campaign registry to ~/.obey/campaign/registry.json.
// Uses atomic write to prevent corruption.
func SaveRegistry(ctx context.Context, reg *Registry) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Ensure config directory exists
	if err := EnsureConfigDir(); err != nil {
		return camperrors.Wrap(err, "failed to create config directory")
	}

	// Set version
	reg.Version = RegistryVersion

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal registry")
	}

	path := RegistryPath()

	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "failed to write registry")
	}

	return nil
}

// rebuildPathIndex rebuilds the path-to-ID lookup index.
func (r *Registry) rebuildPathIndex() {
	r.pathIndex = make(map[string]string)
	for id, c := range r.Campaigns {
		r.pathIndex[c.Path] = id
	}
}

// ErrPathConflict is returned when a path is already registered to a different campaign.
var ErrPathConflict = errors.New("path already registered to different campaign")

// ErrEmptyID is returned when attempting to register with an empty ID.
var ErrEmptyID = errors.New("campaign ID cannot be empty")

// Register adds or updates a campaign in the registry.
// The campaign is keyed by its ID for uniqueness.
// Returns an error if the path is already registered to a different campaign.
func (r *Registry) Register(id, name, path string, campaignType CampaignType) error {
	// Validate: ID must not be empty
	if id == "" {
		return ErrEmptyID
	}

	if r.Campaigns == nil {
		r.Campaigns = make(map[string]RegisteredCampaign)
	}
	if r.pathIndex == nil {
		r.rebuildPathIndex()
	}

	// Check if this path is already registered to a DIFFERENT ID
	if existingID, exists := r.pathIndex[path]; exists && existingID != id {
		return camperrors.Wrap(ErrPathConflict, existingID)
	}

	// If this ID exists with a different path, remove old path from index
	if existing, exists := r.Campaigns[id]; exists && existing.Path != path {
		delete(r.pathIndex, existing.Path)
	}

	r.Campaigns[id] = RegisteredCampaign{
		ID:         id,
		Name:       name,
		Path:       path,
		Type:       campaignType,
		LastAccess: time.Now(),
	}
	r.pathIndex[path] = id

	return nil
}

// UnregisterByID removes a campaign from the registry by ID.
func (r *Registry) UnregisterByID(id string) {
	if r.Campaigns == nil {
		return
	}
	if c, exists := r.Campaigns[id]; exists {
		// Remove from path index
		if r.pathIndex != nil {
			delete(r.pathIndex, c.Path)
		}
		delete(r.Campaigns, id)
	}
}

// UnregisterByName removes a campaign from the registry by name.
// If multiple campaigns have the same name, only the first found is removed.
func (r *Registry) UnregisterByName(name string) bool {
	if r.Campaigns == nil {
		return false
	}
	for id, c := range r.Campaigns {
		if c.Name == name {
			// Remove from path index
			if r.pathIndex != nil {
				delete(r.pathIndex, c.Path)
			}
			delete(r.Campaigns, id)
			return true
		}
	}
	return false
}

// GetByID retrieves a campaign from the registry by its full ID.
// Returns the campaign and true if found, or zero value and false if not.
func (r *Registry) GetByID(id string) (RegisteredCampaign, bool) {
	if r.Campaigns == nil {
		return RegisteredCampaign{}, false
	}
	c, ok := r.Campaigns[id]
	return c, ok
}

// GetByIDPrefix retrieves a campaign by ID prefix (like git commit hashes).
// Returns an error if multiple campaigns match the prefix.
func (r *Registry) GetByIDPrefix(prefix string) (RegisteredCampaign, error) {
	if r.Campaigns == nil {
		return RegisteredCampaign{}, ErrCampaignNotFound
	}

	var matches []RegisteredCampaign
	for id, c := range r.Campaigns {
		if strings.HasPrefix(id, prefix) {
			matches = append(matches, c)
		}
	}

	switch len(matches) {
	case 0:
		return RegisteredCampaign{}, ErrCampaignNotFound
	case 1:
		return matches[0], nil
	default:
		return RegisteredCampaign{}, ErrMultipleMatches
	}
}

// GetByName retrieves a campaign from the registry by name.
// Returns the campaign and true if found, or zero value and false if not.
// If multiple campaigns have the same name, returns the first found.
func (r *Registry) GetByName(name string) (RegisteredCampaign, bool) {
	if r.Campaigns == nil {
		return RegisteredCampaign{}, false
	}
	for _, c := range r.Campaigns {
		if c.Name == name {
			return c, true
		}
	}
	return RegisteredCampaign{}, false
}

// Get retrieves a campaign by ID or name (tries ID first, then name).
// For CLI convenience - accepts either identifier.
func (r *Registry) Get(query string) (RegisteredCampaign, bool) {
	// Try exact ID match first
	if c, ok := r.GetByID(query); ok {
		return c, true
	}
	// Try ID prefix match
	if c, err := r.GetByIDPrefix(query); err == nil {
		return c, true
	}
	// Fall back to name lookup
	return r.GetByName(query)
}

// UpdateLastAccess updates the last access time for a campaign by ID.
func (r *Registry) UpdateLastAccess(id string) {
	if r.Campaigns == nil {
		return
	}
	if c, ok := r.Campaigns[id]; ok {
		c.LastAccess = time.Now()
		r.Campaigns[id] = c
	}
}

// ListIDs returns all campaign IDs in the registry.
func (r *Registry) ListIDs() []string {
	if r.Campaigns == nil {
		return nil
	}
	ids := make([]string, 0, len(r.Campaigns))
	for id := range r.Campaigns {
		ids = append(ids, id)
	}
	return ids
}

// ListAll returns all registered campaigns.
func (r *Registry) ListAll() []RegisteredCampaign {
	if r.Campaigns == nil {
		return nil
	}
	campaigns := make([]RegisteredCampaign, 0, len(r.Campaigns))
	for _, c := range r.Campaigns {
		campaigns = append(campaigns, c)
	}
	return campaigns
}

// List returns all campaign names in the registry.
func (r *Registry) List() []string {
	if r.Campaigns == nil {
		return nil
	}
	names := make([]string, 0, len(r.Campaigns))
	for _, c := range r.Campaigns {
		names = append(names, c.Name)
	}
	return names
}

// Len returns the number of campaigns in the registry.
func (r *Registry) Len() int {
	if r.Campaigns == nil {
		return 0
	}
	return len(r.Campaigns)
}

// FindByPath finds a campaign by its path.
// Returns the campaign if found.
func (r *Registry) FindByPath(path string) (RegisteredCampaign, bool) {
	if r.Campaigns == nil {
		return RegisteredCampaign{}, false
	}

	// Use index if available for O(1) lookup
	if r.pathIndex != nil {
		if id, exists := r.pathIndex[path]; exists {
			if c, ok := r.Campaigns[id]; ok {
				return c, true
			}
		}
		return RegisteredCampaign{}, false
	}

	// Fallback to linear search
	for _, c := range r.Campaigns {
		if c.Path == path {
			return c, true
		}
	}
	return RegisteredCampaign{}, false
}

// VerifyAndRepair validates all registry entries against their campaign.yaml files
// and auto-heals any inconsistencies by removing bad entries and creating correct ones.
func (r *Registry) VerifyAndRepair(ctx context.Context) (*VerificationReport, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	report := &VerificationReport{}

	if r.Campaigns == nil {
		return report, nil
	}

	var toRemove []string
	toAdd := make(map[string]RegisteredCampaign)

	for id, entry := range r.Campaigns {
		report.TotalVerified++

		// Check path exists
		if _, err := os.Stat(entry.Path); os.IsNotExist(err) {
			report.Removed = append(report.Removed, RemovedEntry{
				ID:     id,
				Name:   entry.Name,
				Path:   entry.Path,
				Reason: "path does not exist",
			})
			toRemove = append(toRemove, id)
			continue
		}

		// Check campaign.yaml exists
		configPath := filepath.Join(entry.Path, CampaignDir, CampaignConfigFile)
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			report.Removed = append(report.Removed, RemovedEntry{
				ID:     id,
				Name:   entry.Name,
				Path:   entry.Path,
				Reason: "no campaign.yaml (not a campaign)",
			})
			toRemove = append(toRemove, id)
			continue
		}

		// Load campaign.yaml
		cfg, err := LoadCampaignConfig(ctx, entry.Path)
		if err != nil {
			report.Removed = append(report.Removed, RemovedEntry{
				ID:     id,
				Name:   entry.Name,
				Path:   entry.Path,
				Reason: fmt.Sprintf("invalid campaign.yaml: %v", err),
			})
			toRemove = append(toRemove, id)
			continue
		}

		// Check ID matches
		if id != cfg.ID {
			// Truncate IDs for display
			shortActual := cfg.ID
			if len(shortActual) > 8 {
				shortActual = shortActual[:8]
			}
			report.Removed = append(report.Removed, RemovedEntry{
				ID:     id,
				Name:   entry.Name,
				Path:   entry.Path,
				Reason: fmt.Sprintf("ID mismatch (actual: %s)", shortActual),
			})
			toRemove = append(toRemove, id)

			// Add correct entry if not already tracked
			if _, exists := r.Campaigns[cfg.ID]; !exists {
				if _, alreadyAdding := toAdd[cfg.ID]; !alreadyAdding {
					toAdd[cfg.ID] = RegisteredCampaign{
						ID:         cfg.ID,
						Name:       cfg.Name,
						Path:       entry.Path,
						Type:       cfg.Type,
						LastAccess: entry.LastAccess,
					}
					report.Added = append(report.Added, AddedEntry{
						ID:   cfg.ID,
						Name: cfg.Name,
						Path: entry.Path,
					})
				}
			}
			continue
		}

		// Update Name/Type if changed
		var changes []string
		if entry.Name != cfg.Name {
			changes = append(changes, fmt.Sprintf("name: %q → %q", entry.Name, cfg.Name))
			entry.Name = cfg.Name
		}
		if entry.Type != cfg.Type {
			changes = append(changes, fmt.Sprintf("type: %s → %s", entry.Type, cfg.Type))
			entry.Type = cfg.Type
		}
		if len(changes) > 0 {
			r.Campaigns[id] = entry
			report.Updated = append(report.Updated, UpdatedEntry{
				ID:      id,
				Path:    entry.Path,
				Changes: changes,
			})
		}
	}

	// Execute removals
	for _, id := range toRemove {
		delete(r.Campaigns, id)
	}

	// Execute additions
	for id, entry := range toAdd {
		r.Campaigns[id] = entry
	}

	// Rebuild path index
	r.rebuildPathIndex()

	return report, nil
}
