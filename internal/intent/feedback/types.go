// Package feedback provides feedback gathering from festival observations
// into the intent system.
package feedback

import "time"

// Observation represents a single feedback observation from a festival.
// Compatible with fest's feedback.Observation YAML struct.
type Observation struct {
	ID          string `yaml:"id"`
	Criteria    string `yaml:"criteria"`
	Observation string `yaml:"observation"`
	Task        string `yaml:"task,omitempty"`
	Timestamp   string `yaml:"timestamp"`
	Severity    string `yaml:"severity,omitempty"`
	Suggestion  string `yaml:"suggestion,omitempty"`
}

// FestivalInfo holds metadata extracted from a festival's FESTIVAL_GOAL.md frontmatter.
type FestivalInfo struct {
	ID     string // fest_id (e.g., "CC0004")
	Name   string // fest_name (e.g., "camp-commands-enhancement")
	Status string // Directory status (e.g., "completed", "active", "planned")
	Path   string // Full path to the festival directory
}

// FestivalFeedback groups a festival with its observations.
type FestivalFeedback struct {
	Festival     FestivalInfo
	Observations []Observation
}

// GatheredTracking tracks which observations have already been gathered
// from a festival. Stored at <festival>/.fest/gathered_feedback.yaml.
type GatheredTracking struct {
	Version      int       `yaml:"version"`
	LastGathered time.Time `yaml:"last_gathered"`
	Hashes       []string  `yaml:"hashes"`
}

// GatherOptions configures the feedback gathering behavior.
type GatherOptions struct {
	FestivalID string   // Only gather from a specific festival
	Statuses   []string // Festival status dirs to scan (e.g., "completed", "active", "planned")
	Severity   string   // Filter by observation severity
	Force      bool     // Re-gather all, ignoring tracking
	DryRun     bool     // Preview without creating intents
	NoCommit   bool     // Skip git commit
}

// GatherResult contains the outcome of a feedback gathering operation.
type GatherResult struct {
	FestivalsScanned int
	NewObservations  int
	IntentsCreated   int
	IntentsUpdated   int
	FestivalResults  []FestivalGatherResult
}

// FestivalGatherResult contains per-festival gathering results.
type FestivalGatherResult struct {
	Festival      FestivalInfo
	TotalObs      int
	NewObs        int
	IntentFile    string
	IntentCreated bool
	IntentUpdated bool
}

// GoalFrontmatter represents the YAML frontmatter from FESTIVAL_GOAL.md.
// Only includes fields we need for scanning.
type GoalFrontmatter struct {
	Type   string `yaml:"fest_type"`
	ID     string `yaml:"fest_id"`
	Name   string `yaml:"fest_name"`
	Status string `yaml:"fest_status"`
}
