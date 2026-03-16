package quest

import (
	"strings"
	"time"
)

// Status represents the lifecycle state of a quest.
type Status string

const (
	StatusOpen      Status = "open"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusArchived  Status = "archived"
)

// String returns the string form of the status.
func (s Status) String() string {
	return string(s)
}

const (
	// DefaultQuestID is the fixed ID for the campaign fallback quest.
	DefaultQuestID = "qst_default"
	// DefaultQuestName is the human-readable default quest name.
	DefaultQuestName = "default"
	// RootDirName is the hidden campaign metadata directory for quests.
	RootDirName = ".campaign/quests"
	// FileName is the quest metadata filename for directory-backed quests.
	FileName = "quest.yaml"
	// DefaultFileName stores the special default quest metadata.
	DefaultFileName = "default.yaml"
)

// Link associates a campaign artifact with a quest.
type Link struct {
	Path    string    `yaml:"path" json:"path"`
	Type    string    `yaml:"type" json:"type"`
	AddedAt time.Time `yaml:"added_at" json:"added_at"`
}

// Quest represents one execution context inside a campaign.
type Quest struct {
	ID          string    `yaml:"id" json:"id"`
	Name        string    `yaml:"name" json:"name"`
	Purpose     string    `yaml:"purpose,omitempty" json:"purpose,omitempty"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
	Status      Status    `yaml:"status" json:"status"`
	CreatedAt   time.Time `yaml:"created_at" json:"created_at"`
	UpdatedAt   time.Time `yaml:"updated_at" json:"updated_at"`
	Tags        []string  `yaml:"tags,omitempty" json:"tags,omitempty"`
	Links       []Link    `yaml:"links,omitempty" json:"links,omitempty"`

	Slug string `yaml:"-" json:"slug"`
	Path string `yaml:"-" json:"path"`
}

// Clone returns a deep copy of the quest metadata.
func (q *Quest) Clone() *Quest {
	if q == nil {
		return nil
	}
	clone := *q
	if len(q.Tags) > 0 {
		clone.Tags = append([]string{}, q.Tags...)
	}
	if len(q.Links) > 0 {
		clone.Links = make([]Link, len(q.Links))
		copy(clone.Links, q.Links)
	}
	return &clone
}

// IsDefault reports whether the quest is the fixed campaign default quest.
func (q *Quest) IsDefault() bool {
	return q != nil && q.ID == DefaultQuestID
}

// Validate returns a user-facing validation error, or nil if valid.
func (q *Quest) Validate() error {
	if q == nil {
		return ErrInvalidQuest
	}
	if q.ID == "" {
		return ErrMissingID
	}
	if q.Name == "" {
		return ErrMissingName
	}
	if !q.Status.Valid() {
		return ErrInvalidStatus
	}
	if q.CreatedAt.IsZero() {
		return ErrMissingCreatedAt
	}
	if q.UpdatedAt.IsZero() {
		return ErrMissingUpdatedAt
	}
	return nil
}

// Valid reports whether the status is supported.
func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusPaused, StatusCompleted, StatusArchived:
		return true
	default:
		return false
	}
}

// InDungeon reports whether the status lives under the quest dungeon.
func (s Status) InDungeon() bool {
	return s == StatusCompleted || s == StatusArchived
}

// ActiveStatuses returns the non-dungeon statuses.
func ActiveStatuses() []Status {
	return []Status{StatusOpen, StatusPaused}
}

// AllStatuses returns all supported statuses.
func AllStatuses() []Status {
	return []Status{StatusOpen, StatusPaused, StatusCompleted, StatusArchived}
}

// ParseStatus converts a string flag value into a quest status.
func ParseStatus(raw string) (Status, error) {
	status := Status(strings.TrimSpace(strings.ToLower(raw)))
	if status.Valid() {
		return status, nil
	}
	return "", ErrInvalidStatus
}
