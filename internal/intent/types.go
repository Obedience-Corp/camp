// Package intent provides types and operations for managing intents,
// which are captured ideas, feature requests, or work items that may
// eventually be promoted to Festivals.
package intent

import (
	"path/filepath"
	"strings"
	"time"
)

// Status represents the lifecycle state of an intent.
type Status string

const (
	// StatusInbox indicates the intent has been captured but not reviewed.
	StatusInbox Status = "inbox"

	// StatusActive indicates the intent is being enriched by humans or agents.
	StatusActive Status = "active"

	// StatusReady indicates the intent is sufficiently clear for Festival promotion.
	StatusReady Status = "ready"

	// StatusDone indicates the intent has been resolved (promoted, completed, or superseded).
	StatusDone Status = "done"

	// StatusKilled indicates the intent has been abandoned.
	StatusKilled Status = "killed"
)

// String returns the string representation of Status.
func (s Status) String() string {
	return string(s)
}

// Type categorizes the nature of work described by an intent.
type Type string

const (
	// TypeIdea represents a general idea or suggestion.
	TypeIdea Type = "idea"

	// TypeFeature represents new functionality.
	TypeFeature Type = "feature"

	// TypeBug represents a defect or issue to fix.
	TypeBug Type = "bug"

	// TypeResearch represents investigation or exploration.
	TypeResearch Type = "research"

	// TypeChore represents maintenance or cleanup work.
	TypeChore Type = "chore"

	// TypeFeedback represents feedback gathered from festival observations.
	TypeFeedback Type = "feedback"
)

// String returns the string representation of Type.
func (t Type) String() string {
	return string(t)
}

// Priority indicates the urgency level of an intent.
type Priority string

const (
	// PriorityLow indicates nice to have, no urgency.
	PriorityLow Priority = "low"

	// PriorityMedium indicates standard priority.
	PriorityMedium Priority = "medium"

	// PriorityHigh indicates should be addressed soon.
	PriorityHigh Priority = "high"
)

// String returns the string representation of Priority.
func (p Priority) String() string {
	return string(p)
}

// Horizon represents the time horizon for when work should be considered.
type Horizon string

// GatheredSource preserves the full metadata of an intent that was merged
// into a gathered intent. This allows tracing the lineage of gathered ideas.
type GatheredSource struct {
	// Identity
	ID       string `yaml:"id"`
	Title    string `yaml:"title"`
	Filename string `yaml:"filename"` // Original filename

	// Timestamps
	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at,omitempty"`

	// Classification
	Type     Type     `yaml:"type,omitempty"`
	Concept  string   `yaml:"concept,omitempty"`
	Priority Priority `yaml:"priority,omitempty"`
	Horizon  Horizon  `yaml:"horizon,omitempty"`

	// Organization
	Tags   []string `yaml:"tags,omitempty"`
	Author string   `yaml:"author,omitempty"`

	// Dependencies (preserved for reference)
	BlockedBy []string `yaml:"blocked_by,omitempty"`
	DependsOn []string `yaml:"depends_on,omitempty"`
}

const (
	// HorizonNow indicates current focus area.
	HorizonNow Horizon = "now"

	// HorizonNext indicates work to be done after current work completes.
	HorizonNext Horizon = "next"

	// HorizonLater indicates future consideration.
	HorizonLater Horizon = "later"
)

// String returns the string representation of Horizon.
func (h Horizon) String() string {
	return string(h)
}

// Intent represents a captured idea, feature request, or work item
// that may eventually be promoted to a Festival.
type Intent struct {
	// Required fields
	ID        string    `yaml:"id"`
	Title     string    `yaml:"title"`
	Status    Status    `yaml:"status"`
	CreatedAt time.Time `yaml:"created_at"`

	// Optional metadata
	Type     Type     `yaml:"type,omitempty"`
	Concept  string   `yaml:"concept,omitempty"` // Full concept path: "projects/camp"
	Author   string   `yaml:"author,omitempty"`
	Priority Priority `yaml:"priority,omitempty"`
	Horizon  Horizon  `yaml:"horizon,omitempty"`
	Tags     []string `yaml:"tags,omitempty"`

	// Dependencies (can reference Intent IDs or Festival IDs)
	BlockedBy []string `yaml:"blocked_by,omitempty"`
	DependsOn []string `yaml:"depends_on,omitempty"`

	// Promotion
	PromotionCriteria string `yaml:"promotion_criteria,omitempty"`
	PromotedTo        string `yaml:"promoted_to,omitempty"`

	// Gathering - when this intent was created by merging others
	GatheredFrom []GatheredSource `yaml:"gathered_from,omitempty"`
	GatheredAt   time.Time        `yaml:"gathered_at,omitempty"`

	// GatheredInto - when this intent was merged into another (set on archived sources)
	GatheredInto string `yaml:"gathered_into,omitempty"`

	// Tracking
	UpdatedAt time.Time `yaml:"updated_at,omitempty"`

	// Runtime fields (not serialized to YAML)
	Path    string `yaml:"-"` // Filesystem path to the intent file
	Content string `yaml:"-"` // Markdown body content (after frontmatter)
}

// ConceptType returns the concept type from the path (e.g., "projects" from "projects/camp").
func (i *Intent) ConceptType() string {
	if i.Concept == "" {
		return ""
	}
	parts := strings.SplitN(i.Concept, "/", 2)
	return parts[0]
}

// ConceptName returns the concept name from the path (e.g., "camp" from "projects/camp").
func (i *Intent) ConceptName() string {
	if i.Concept == "" {
		return ""
	}
	return filepath.Base(i.Concept)
}
