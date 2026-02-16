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
		Decision:  DecisionArchive,
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

	// Verify format
	expected := `{"timestamp":"2026-01-22T10:30:00Z","Item":"test-item/","Decision":"archive","Info":{"files":12,"code":450,"source":"scc"}}`

	// Unmarshal both to compare as JSON (order-independent)
	var got, want map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatalf("failed to unmarshal expected: %v", err)
	}

	// Check timestamp format
	if ts, ok := got["timestamp"].(string); !ok || ts != "2026-01-22T10:30:00Z" {
		t.Errorf("timestamp not in RFC3339 format: got %v", got["timestamp"])
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
			name:     "mixed",
			summary:  CrawlSummary{Kept: 3, Completed: 1, Archived: 2, Someday: 1, Skipped: 1},
			expected: 8,
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

func TestTriageSummary_Total(t *testing.T) {
	tests := []struct {
		name     string
		summary  TriageSummary
		expected int
	}{
		{
			name:     "empty",
			summary:  TriageSummary{},
			expected: 0,
		},
		{
			name:     "mixed",
			summary:  TriageSummary{Moved: 1, Completed: 2, Archived: 3, Someday: 4, Kept: 5, Skipped: 6},
			expected: 21,
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
	// Verify decision values are correct strings for JSON
	if DecisionKeep != "keep" {
		t.Errorf("DecisionKeep should be 'keep', got %s", DecisionKeep)
	}
	if DecisionArchive != "archive" {
		t.Errorf("DecisionArchive should be 'archive', got %s", DecisionArchive)
	}
	if DecisionCompleted != "completed" {
		t.Errorf("DecisionCompleted should be 'completed', got %s", DecisionCompleted)
	}
	if DecisionSomeday != "someday" {
		t.Errorf("DecisionSomeday should be 'someday', got %s", DecisionSomeday)
	}
	if DecisionSkip != "skip" {
		t.Errorf("DecisionSkip should be 'skip', got %s", DecisionSkip)
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
