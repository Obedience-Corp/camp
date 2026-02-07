package feedback

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Tracker manages deduplication tracking for gathered feedback observations.
// It stores hashes of already-gathered observations in <festival>/.fest/gathered_feedback.yaml.
type Tracker struct{}

// NewTracker creates a new deduplication tracker.
func NewTracker() *Tracker {
	return &Tracker{}
}

// HashObservation computes a SHA-256 hash of an observation's semantic content.
// Hash is based on criteria + observation + suggestion (excludes timestamp and severity).
func HashObservation(obs *Observation) string {
	input := obs.Criteria + "|" + obs.Observation + "|" + obs.Suggestion
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)
}

// Load reads the tracking file for a festival directory.
// Returns nil tracking if the file doesn't exist.
func (t *Tracker) Load(festivalDir string) (*GatheredTracking, error) {
	path := trackingPath(festivalDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading tracking file: %w", err)
	}

	var tracking GatheredTracking
	if err := yaml.Unmarshal(data, &tracking); err != nil {
		return nil, fmt.Errorf("parsing tracking file: %w", err)
	}

	return &tracking, nil
}

// Save writes the tracking file for a festival directory.
func (t *Tracker) Save(festivalDir string, tracking *GatheredTracking) error {
	path := trackingPath(festivalDir)

	// Ensure .fest directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating .fest directory: %w", err)
	}

	data, err := yaml.Marshal(tracking)
	if err != nil {
		return fmt.Errorf("marshaling tracking: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing tracking file: %w", err)
	}

	return nil
}

// FilterNew returns only observations that haven't been gathered yet.
// If force is true, returns all observations.
func (t *Tracker) FilterNew(festivalDir string, observations []Observation, force bool) ([]Observation, error) {
	if force {
		return observations, nil
	}

	tracking, err := t.Load(festivalDir)
	if err != nil {
		return nil, err
	}

	// If no tracking file, all observations are new
	if tracking == nil {
		return observations, nil
	}

	// Build hash set for quick lookup
	seen := make(map[string]struct{}, len(tracking.Hashes))
	for _, h := range tracking.Hashes {
		seen[h] = struct{}{}
	}

	var newObs []Observation
	for i := range observations {
		hash := HashObservation(&observations[i])
		if _, exists := seen[hash]; !exists {
			newObs = append(newObs, observations[i])
		}
	}

	return newObs, nil
}

// RecordGathered updates the tracking file with newly gathered observation hashes.
func (t *Tracker) RecordGathered(festivalDir string, observations []Observation) error {
	tracking, err := t.Load(festivalDir)
	if err != nil {
		return err
	}

	if tracking == nil {
		tracking = &GatheredTracking{
			Version: 1,
		}
	}

	// Add new hashes
	for i := range observations {
		hash := HashObservation(&observations[i])
		tracking.Hashes = append(tracking.Hashes, hash)
	}

	tracking.LastGathered = time.Now()

	return t.Save(festivalDir, tracking)
}

// trackingPath returns the path to the tracking file for a festival.
func trackingPath(festivalDir string) string {
	return filepath.Join(festivalDir, ".fest", "gathered_feedback.yaml")
}
