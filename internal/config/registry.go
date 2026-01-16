package config

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadRegistry loads the campaign registry from ~/.config/campaign/registry.yaml.
// Returns an empty registry if the file doesn't exist.
func LoadRegistry(ctx context.Context) (*Registry, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	path := RegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewRegistry(), nil
		}
		return nil, fmt.Errorf("failed to read registry %s: %w", path, err)
	}

	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse registry %s: %w", path, err)
	}

	// Initialize map if nil
	if reg.Campaigns == nil {
		reg.Campaigns = make(map[string]RegisteredCampaign)
	}

	return &reg, nil
}

// SaveRegistry saves the campaign registry to ~/.config/campaign/registry.yaml.
// Uses atomic write to prevent corruption.
func SaveRegistry(ctx context.Context, reg *Registry) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Ensure config directory exists
	if err := EnsureConfigDir(); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(reg)
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

// Register adds or updates a campaign in the registry.
func (r *Registry) Register(name, path string, campaignType CampaignType) {
	if r.Campaigns == nil {
		r.Campaigns = make(map[string]RegisteredCampaign)
	}
	r.Campaigns[name] = RegisteredCampaign{
		Path:       path,
		Type:       campaignType,
		LastAccess: time.Now(),
	}
}

// Unregister removes a campaign from the registry.
func (r *Registry) Unregister(name string) {
	if r.Campaigns != nil {
		delete(r.Campaigns, name)
	}
}

// Get retrieves a campaign from the registry.
// Returns the campaign and true if found, or zero value and false if not.
func (r *Registry) Get(name string) (RegisteredCampaign, bool) {
	if r.Campaigns == nil {
		return RegisteredCampaign{}, false
	}
	c, ok := r.Campaigns[name]
	return c, ok
}

// UpdateLastAccess updates the last access time for a campaign.
func (r *Registry) UpdateLastAccess(name string) {
	if r.Campaigns == nil {
		return
	}
	if c, ok := r.Campaigns[name]; ok {
		c.LastAccess = time.Now()
		r.Campaigns[name] = c
	}
}

// List returns all campaign names in the registry.
func (r *Registry) List() []string {
	if r.Campaigns == nil {
		return nil
	}
	names := make([]string, 0, len(r.Campaigns))
	for name := range r.Campaigns {
		names = append(names, name)
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
// Returns the name and campaign if found, or empty string and zero value if not.
func (r *Registry) FindByPath(path string) (string, RegisteredCampaign, bool) {
	if r.Campaigns == nil {
		return "", RegisteredCampaign{}, false
	}
	for name, c := range r.Campaigns {
		if c.Path == path {
			return name, c, true
		}
	}
	return "", RegisteredCampaign{}, false
}
