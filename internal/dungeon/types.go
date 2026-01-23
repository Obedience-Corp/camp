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
	DecisionKeep    Decision = "keep"
	DecisionArchive Decision = "archive"
	DecisionSkip    Decision = "skip"
)

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
	Timestamp time.Time   `json:"timestamp"`
	Item      string      `json:"item"`
	Decision  Decision    `json:"decision"`
	Info      *ItemStats  `json:"info,omitempty"`
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

// CrawlSummary contains the results of a crawl session.
type CrawlSummary struct {
	Kept     int
	Archived int
	Skipped  int
}

// Total returns the total number of items processed.
func (s CrawlSummary) Total() int {
	return s.Kept + s.Archived + s.Skipped
}
