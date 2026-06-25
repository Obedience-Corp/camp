// Package priority manages persistent manual priority state for campaign work items.
// Priority entries are stored in .campaign/settings/workitems.json keyed by WorkItem.Key.
package priority

import "time"

// ManualPriority represents a user-assigned importance level for a work item.
type ManualPriority string

const (
	None   ManualPriority = ""
	Low    ManualPriority = "low"
	Medium ManualPriority = "medium"
	High   ManualPriority = "high"
)

type AttentionStage string

const (
	AttentionNone    AttentionStage = ""
	AttentionCurrent AttentionStage = "current"
	AttentionNext    AttentionStage = "next"
	AttentionActive  AttentionStage = "active"
	AttentionParked  AttentionStage = "parked"
)

// Rank returns a sort-order integer: High=1, Medium=2, Low=3, None/unknown=4.
func (p ManualPriority) Rank() int {
	switch p {
	case High:
		return 1
	case Medium:
		return 2
	case Low:
		return 3
	default:
		return 4
	}
}

// Valid returns true for assignable priorities (Low, Medium, High). None is not valid
// because it represents the absence of a manual priority.
func (p ManualPriority) Valid() bool {
	return p == Low || p == Medium || p == High
}

// PriorityEntry is a single priority assignment persisted on disk.
type PriorityEntry struct {
	Priority  ManualPriority `json:"priority"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type AttentionEntry struct {
	Stage     AttentionStage `json:"stage,omitempty"`
	Group     string         `json:"group,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Store is the on-disk JSON representation of all shared workitem settings.
type Store struct {
	Version          int                       `json:"version"`
	ManualPriorities map[string]PriorityEntry  `json:"manual_priorities"`
	Attention        map[string]AttentionEntry `json:"attention,omitempty"`
}

// NewStore returns an initialized Store ready for use.
func NewStore() *Store {
	return &Store{
		Version:          2,
		ManualPriorities: make(map[string]PriorityEntry),
		Attention:        make(map[string]AttentionEntry),
	}
}

func (s AttentionStage) Valid() bool {
	return s == AttentionCurrent || s == AttentionNext || s == AttentionActive || s == AttentionParked
}

func (s AttentionStage) Rank() int {
	switch s {
	case AttentionCurrent:
		return 1
	case AttentionNext:
		return 2
	case AttentionActive:
		return 3
	case AttentionParked:
		return 4
	default:
		return 5
	}
}
