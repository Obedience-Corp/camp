// Package flow provides workflow flow registry and execution.
package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Registry holds the collection of named workflow flows.
type Registry struct {
	Version int             `yaml:"version"`
	Flows   map[string]Flow `yaml:"flows"`
}

// Flow represents a single named workflow with its execution details.
type Flow struct {
	Description string            `yaml:"description"`
	Command     string            `yaml:"command"`
	WorkDir     string            `yaml:"workdir"`
	Tags        []string          `yaml:"tags,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
}

// RegistryPath returns the absolute path to the flow registry file.
func RegistryPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, ".campaign", "flows", "registry.yaml")
}

// LoadRegistry loads the flow registry from .campaign/flows/registry.yaml.
// If the file doesn't exist, returns an empty registry with version 1.
func LoadRegistry(campaignRoot string) (*Registry, error) {
	path := RegistryPath(campaignRoot)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &Registry{
			Version: 1,
			Flows:   make(map[string]Flow),
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading registry file: %w", err)
	}

	var registry Registry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("parsing registry YAML: %w", err)
	}

	if registry.Flows == nil {
		registry.Flows = make(map[string]Flow)
	}

	return &registry, nil
}

// SaveRegistry saves the flow registry to .campaign/flows/registry.yaml.
func SaveRegistry(campaignRoot string, registry *Registry) error {
	path := RegistryPath(campaignRoot)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating registry directory: %w", err)
	}

	data, err := yaml.Marshal(registry)
	if err != nil {
		return fmt.Errorf("marshaling registry to YAML: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing registry file: %w", err)
	}

	return nil
}

// List returns all flow names in sorted order.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.Flows))
	for name := range r.Flows {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get retrieves a flow by name.
func (r *Registry) Get(name string) (Flow, bool) {
	flow, ok := r.Flows[name]
	return flow, ok
}
