// Package intent provides types and operations for managing intents,
// which are captured ideas, feature requests, or work items that may
// eventually be promoted to Festivals.
//
// Intents follow a lifecycle from inbox → ready → active, then move to
// one of four dungeon statuses: done (resolved), killed (abandoned),
// archived (preserved but inactive), or someday (deferred).
// The dungeon/ directory hierarchy mirrors the campaign-wide dungeon pattern.
package intent

import (
	"path/filepath"
	"strings"
	"time"
)

// Status represents the lifecycle state of an intent.
// Status values double as directory paths relative to the intents root —
// active statuses map to top-level directories (e.g. "inbox"),
// while dungeon statuses include the "dungeon/" prefix (e.g. "dungeon/done").
type Status string

const (
	// Active statuses (top-level directories)

	// StatusInbox indicates the intent has been captured but not reviewed.
	StatusInbox Status = "inbox"

	// StatusReady indicates the intent has been reviewed/enriched and is ready
	// to be promoted out to a festival or design doc.
	StatusReady Status = "ready"

	// StatusActive indicates the intent has been promoted out to a festival or
	// design doc and work is in progress.
	StatusActive Status = "active"

	// Dungeon statuses (under dungeon/ directory)

	// StatusDone indicates the intent has been resolved (completed or superseded).
	StatusDone Status = "dungeon/done"

	// StatusKilled indicates the intent has been abandoned.
	StatusKilled Status = "dungeon/killed"

	// StatusArchived indicates the intent has been preserved but is no longer active.
	StatusArchived Status = "dungeon/archived"

	// StatusSomeday indicates the intent is deferred — maybe later, low priority.
	StatusSomeday Status = "dungeon/someday"

	// Note statuses (under notes/ directory)

	// StatusNote is the flat note store. Notes are a separate category that sits
	// outside the inbox/ready/active intent lifecycle.
	StatusNote Status = "notes"

	// StatusNoteArchived is the archived-note bucket: notes kept but hidden.
	StatusNoteArchived Status = "notes/archived"
)

// Category distinguishes the two kinds of captured item: action-oriented
// intents that flow through the inbox/ready/active lifecycle, and freeform
// notes that live in a flat notes/ store outside that lifecycle. The directory
// a file lives in is the source of truth for its category.
type Category string

const (
	// CategoryIntent is the default category: an actionable intent.
	CategoryIntent Category = "intent"

	// CategoryNote is a freeform note, stored under notes/.
	CategoryNote Category = "note"
)

// String returns the string representation of Category.
func (c Category) String() string {
	return string(c)
}

// String returns the string representation of Status.
func (s Status) String() string {
	return string(s)
}

// InDungeon returns true if the status is a dungeon state.
// Intents in dungeon states are not eligible for gathering or indexing.
func (s Status) InDungeon() bool {
	return strings.HasPrefix(string(s), "dungeon/")
}

// AllStatuses returns all valid intent statuses.
func AllStatuses() []Status {
	return []Status{
		StatusInbox, StatusReady, StatusActive,
		StatusDone, StatusKilled, StatusArchived, StatusSomeday,
	}
}

// ActiveStatuses returns the non-dungeon statuses (the working set).
func ActiveStatuses() []Status {
	return []Status{StatusInbox, StatusReady, StatusActive}
}

// DungeonStatuses returns only the dungeon statuses.
func DungeonStatuses() []Status {
	return []Status{StatusDone, StatusKilled, StatusArchived, StatusSomeday}
}

// NoteStatuses returns the note-category directories (flat active + archived).
// These are intentionally excluded from AllStatuses so intent queries never
// scan notes.
func NoteStatuses() []Status {
	return []Status{StatusNote, StatusNoteArchived}
}

// IsNote returns true if the status belongs to the note category.
func (s Status) IsNote() bool {
	return s == StatusNote || strings.HasPrefix(string(s), string(StatusNote)+"/")
}

// Category returns the category implied by the status. The note directories map
// to CategoryNote; everything else is an intent.
func (s Status) Category() Category {
	if s.IsNote() {
		return CategoryNote
	}
	return CategoryIntent
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

	// HorizonSomeday indicates no specific timeframe — do it if/when it makes sense.
	HorizonSomeday Horizon = "someday"
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
	Path              string             `yaml:"-"` // Filesystem path to the intent file
	Content           string             `yaml:"-"` // Markdown body content (after frontmatter)
	frontmatterExtras []frontmatterEntry `yaml:"-"` // Unknown frontmatter keys preserved across writes
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
