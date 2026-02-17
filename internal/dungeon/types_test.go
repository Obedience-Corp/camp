package dungeon

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCrawlEntry_MarshalJSON(t *testing.T) {
	entry := CrawlEntry{
		Timestamp: time.Date(2026, 1, 22, 10, 30, 0, 0, time.UTC),
		Item:      "test-item/",
		Decision:  MoveDecision("archived"),
		Info: &ItemStats{
			Files:  12,
			Code:   450,
			Source: "scc",
		},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	// Unmarshal to check structure
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Check timestamp format
	if ts, ok := got["timestamp"].(string); !ok || ts != "2026-01-22T10:30:00Z" {
		t.Errorf("timestamp not in RFC3339 format: got %v", got["timestamp"])
	}

	// Check decision value (json tag is lowercase "decision")
	if d, ok := got["decision"].(string); !ok || d != "move:archived" {
		t.Errorf("decision should be 'move:archived', got %v", got["decision"])
	}
}

func TestCrawlSummary_Total(t *testing.T) {
	tests := []struct {
		name     string
		summary  CrawlSummary
		expected int
	}{
		{
			name:     "empty",
			summary:  CrawlSummary{},
			expected: 0,
		},
		{
			name:     "all kept",
			summary:  CrawlSummary{Kept: 5},
			expected: 5,
		},
		{
			name: "mixed with status counts",
			summary: CrawlSummary{
				Kept:         3,
				Skipped:      1,
				StatusCounts: map[string]int{"completed": 1, "archived": 2, "someday": 1},
			},
			expected: 8,
		},
		{
			name: "nil status counts",
			summary: CrawlSummary{
				Kept:    2,
				Skipped: 3,
			},
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.summary.Total(); got != tt.expected {
				t.Errorf("Total() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestDecision_Values(t *testing.T) {
	if DecisionKeep != "keep" {
		t.Errorf("DecisionKeep should be 'keep', got %s", DecisionKeep)
	}
	if DecisionSkip != "skip" {
		t.Errorf("DecisionSkip should be 'skip', got %s", DecisionSkip)
	}
}

func TestMoveDecision(t *testing.T) {
	tests := []struct {
		status   string
		expected Decision
	}{
		{"completed", Decision("move:completed")},
		{"archived", Decision("move:archived")},
		{"someday", Decision("move:someday")},
		{"ready", Decision("move:ready")},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := MoveDecision(tt.status)
			if got != tt.expected {
				t.Errorf("MoveDecision(%q) = %q, want %q", tt.status, got, tt.expected)
			}
		})
	}
}

func TestItemType_Values(t *testing.T) {
	if ItemTypeFile != "file" {
		t.Errorf("ItemTypeFile should be 'file', got %s", ItemTypeFile)
	}
	if ItemTypeDirectory != "directory" {
		t.Errorf("ItemTypeDirectory should be 'directory', got %s", ItemTypeDirectory)
	}
}
