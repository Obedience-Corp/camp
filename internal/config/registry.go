package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrMultipleMatches is returned when an ID prefix matches multiple campaigns.
var ErrMultipleMatches = errors.New("multiple campaigns match that prefix")

// ErrCampaignNotFound is returned when a campaign cannot be found.
var ErrCampaignNotFound = errors.New("campaign not found")

// LoadRegistry loads the campaign registry from ~/.config/campaign/registry.json.
// Returns an empty registry if the file doesn't exist.
// Automatically migrates from YAML format if needed.
func LoadRegistry(ctx context.Context) (*Registry, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	path := RegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Check for legacy YAML file to migrate
			return migrateFromYAMLIfNeeded(ctx)
		}
		return nil, fmt.Errorf("failed to read registry %s: %w", path, err)
	}

	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse registry %s: %w", path, err)
	}

	// Initialize map if nil
	if reg.Campaigns == nil {
		reg.Campaigns = make(map[string]RegisteredCampaign)
	}

	// Set version if not present
	if reg.Version == 0 {
		reg.Version = RegistryVersion
	}

	// Populate IDs from map keys (ID field is not serialized in JSON)
	for id, c := range reg.Campaigns {
		c.ID = id
		reg.Campaigns[id] = c
	}

	// Build path index
	reg.rebuildPathIndex()

	return &reg, nil
}

// migrateFromYAMLIfNeeded checks for and migrates a legacy YAML registry.
func migrateFromYAMLIfNeeded(ctx context.Context) (*Registry, error) {
	legacyPath := LegacyRegistryPath()
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No legacy file, return new registry
			return NewRegistry(), nil
		}
		return nil, fmt.Errorf("failed to read legacy registry %s: %w", legacyPath, err)
	}

	// Parse YAML
	var legacyReg Registry
	if err := yaml.Unmarshal(data, &legacyReg); err != nil {
		return nil, fmt.Errorf("failed to parse legacy registry %s: %w", legacyPath, err)
	}

	// Initialize and deduplicate
	if legacyReg.Campaigns == nil {
		legacyReg.Campaigns = make(map[string]RegisteredCampaign)
	}

	// Deduplicate: if same path appears with different IDs, keep the most recently accessed
	pathToID := make(map[string]string)
	for id, c := range legacyReg.Campaigns {
		c.ID = id
		if existingID, exists := pathToID[c.Path]; exists {
			// Same path with different ID - keep the one with most recent access
			existing := legacyReg.Campaigns[existingID]
			if c.LastAccess.After(existing.LastAccess) {
				// Remove old entry, keep new one
				delete(legacyReg.Campaigns, existingID)
				pathToID[c.Path] = id
			} else {
				// Keep old entry, remove this one
				delete(legacyReg.Campaigns, id)
			}
		} else {
			pathToID[c.Path] = id
		}
		legacyReg.Campaigns[id] = c
	}

	// Set version
	legacyReg.Version = RegistryVersion

	// Build path index
	legacyReg.rebuildPathIndex()

	// Save as JSON
	if err := SaveRegistry(ctx, &legacyReg); err != nil {
		return nil, fmt.Errorf("failed to migrate registry: %w", err)
	}

	// Rename old file to backup
	backupPath := legacyPath + ".backup"
	_ = os.Rename(legacyPath, backupPath) // Ignore error if rename fails

	return &legacyReg, nil
}

// SaveRegistry saves the campaign registry to ~/.config/campaign/registry.json.
// Uses atomic write to prevent corruption.
func SaveRegistry(ctx context.Context, reg *Registry) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Ensure config directory exists
	if err := EnsureConfigDir(); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Set version
	reg.Version = RegistryVersion

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	path := RegistryPath()

	// Atomic write via temp file
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // Clean up temp file on rename failure
		return fmt.Errorf("failed to save registry: %w", err)
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
		return fmt.Errorf("%w: %s", ErrPathConflict, existingID)
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
