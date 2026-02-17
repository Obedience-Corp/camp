// Package dungeon provides management for the campaign dungeon directory.
// The dungeon is a holding area for work you're unsure about or want out of the way.
package dungeon

import (
	"encoding/json"
	"time"
)

// Decision represents the user's choice during crawl.
type Decision string

const (
	DecisionKeep Decision = "keep"
	DecisionSkip Decision = "skip"
)

// MoveDecision returns a Decision for moving an item to a status directory.
func MoveDecision(status string) Decision {
	return Decision("move:" + status)
}

// ItemType identifies whether an item is a file or directory.
type ItemType string

const (
	ItemTypeFile      ItemType = "file"
	ItemTypeDirectory ItemType = "directory"
)

// DungeonItem represents a file or directory in the dungeon root.
type DungeonItem struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Type    ItemType  `json:"type"`
	ModTime time.Time `json:"mod_time"`
}

// ItemStats contains statistics gathered from scc or fest count.
type ItemStats struct {
	Files  int    `json:"files,omitempty"`
	Lines  int    `json:"lines,omitempty"`
	Code   int    `json:"code,omitempty"`
	Tokens int    `json:"tokens,omitempty"`
	Source string `json:"source,omitempty"` // "scc", "fest", or ""
}

// CrawlEntry represents a single decision logged during crawl.
type CrawlEntry struct {
	Timestamp time.Time  `json:"timestamp"`
	Item      string     `json:"item"`
	Decision  Decision   `json:"decision"`
	Info      *ItemStats `json:"info,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for CrawlEntry.
func (e CrawlEntry) MarshalJSON() ([]byte, error) {
	type Alias CrawlEntry
	return json.Marshal(&struct {
		Timestamp string `json:"timestamp"`
		*Alias
	}{
		Timestamp: e.Timestamp.Format(time.RFC3339),
		Alias:     (*Alias)(&e),
	})
}

// StatusDir represents a status subdirectory within the dungeon.
type StatusDir struct {
	Name      string // Directory name (e.g., "completed")
	Path      string // Full path to the directory
	ItemCount int    // Number of items (excluding .gitkeep)
}

// CrawlSummary contains the results of a crawl session.
type CrawlSummary struct {
	Kept         int
	Skipped      int
	StatusCounts map[string]int
}

// Total returns the total number of items processed.
func (s CrawlSummary) Total() int {
	total := s.Kept + s.Skipped
	for _, count := range s.StatusCounts {
		total += count
	}
	return total
}
