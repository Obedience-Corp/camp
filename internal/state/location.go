package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// StateFile is the name of the state file in .campaign/
const StateFile = "state.yaml"

// State represents the campaign navigation state.
type State struct {
	LastLocation   string    `yaml:"last_location,omitempty"`
	LastNavigation time.Time `yaml:"last_navigation,omitempty"`
}

// StatePath returns the path to the state file for a given campaign root.
func StatePath(campaignRoot string) string {
	return filepath.Join(campaignRoot, ".campaign", StateFile)
}

// LoadState loads the navigation state from the campaign root.
// Returns an empty State if the file doesn't exist (no error).
// Returns error only for actual I/O or parsing problems.
func LoadState(ctx context.Context, campaignRoot string) (*State, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	stateFile := StatePath(campaignRoot)
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			// No state file yet - return empty state
			return &State{}, nil
		}
		// Other errors (permissions, etc.) - return as-is
		return nil, fmt.Errorf("failed to read state file %s: %w", stateFile, err)
	}

	var state State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file %s: %w", stateFile, err)
	}

	return &state, nil
}

// SaveState saves the navigation state to the campaign root.
// Creates .campaign/ directory if it doesn't exist.
func SaveState(ctx context.Context, campaignRoot string, state *State) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Update the navigation timestamp
	state.LastNavigation = time.Now()

	stateFile := StatePath(campaignRoot)
	stateDir := filepath.Dir(stateFile)

	// Ensure .campaign/ directory exists
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create campaign directory: %w", err)
	}

	data, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file %s: %w", stateFile, err)
	}

	return nil
}

// SetLastLocation updates the last location for a campaign.
// This is a convenience function that loads current state, updates location, and saves.
func SetLastLocation(ctx context.Context, campaignRoot, location string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Validate that the location exists
	if info, err := os.Stat(location); err != nil || !info.IsDir() {
		return fmt.Errorf("invalid location: %s does not exist or is not a directory", location)
	}

	state, err := LoadState(ctx, campaignRoot)
	if err != nil {
		return err
	}

	state.LastLocation = location
	return SaveState(ctx, campaignRoot, state)
}

// GetLastLocation retrieves the last location from campaign state.
// Returns empty string if no location has been saved yet.
// Returns error only for I/O or parsing problems, not missing state.
func GetLastLocation(ctx context.Context, campaignRoot string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	state, err := LoadState(ctx, campaignRoot)
	if err != nil {
		return "", err
	}

	return state.LastLocation, nil
}

// ClearState removes the state file, resetting navigation history.
// Returns nil if the file doesn't exist (idempotent).
func ClearState(ctx context.Context, campaignRoot string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	stateFile := StatePath(campaignRoot)
	if err := os.Remove(stateFile); err != nil {
		if os.IsNotExist(err) {
			return nil // Idempotent - no state to clear
		}
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}
