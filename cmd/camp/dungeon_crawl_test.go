package main

import (
	"strings"
	"testing"

	"github.com/obediencecorp/camp/internal/dungeon"
)

func TestBuildCrawlCommitMessage(t *testing.T) {
	tests := []struct {
		name         string
		campaignRoot string
		cwd          string
		triage       *dungeon.CrawlSummary
		inner        *dungeon.CrawlSummary
		contains     []string
		notContains  []string
	}{
		{
			name:         "triage only",
			campaignRoot: "/home/user/campaign",
			cwd:          "/home/user/campaign/workflow/design",
			triage: &dungeon.CrawlSummary{
				StatusCounts: map[string]int{"completed": 2},
				MovedItems: map[string][]string{
					"completed": {"old-feature/", "done-thing.md"},
				},
			},
			inner: nil,
			contains: []string{
				"Moved to dungeon/completed:",
				"workflow/design/old-feature/",
				"workflow/design/done-thing.md",
			},
		},
		{
			name:         "inner only",
			campaignRoot: "/home/user/campaign",
			cwd:          "/home/user/campaign/workflow/design",
			triage:       nil,
			inner: &dungeon.CrawlSummary{
				StatusCounts: map[string]int{"archived": 1},
				MovedItems: map[string][]string{
					"archived": {"deprecated.md"},
				},
			},
			contains: []string{
				"Moved to dungeon/archived:",
				"workflow/design/deprecated.md",
			},
		},
		{
			name:         "multiple statuses sorted",
			campaignRoot: "/home/user/campaign",
			cwd:          "/home/user/campaign/docs",
			triage: &dungeon.CrawlSummary{
				StatusCounts: map[string]int{"someday": 1, "completed": 1},
				MovedItems: map[string][]string{
					"someday":   {"maybe-later.md"},
					"completed": {"finished.md"},
				},
			},
			inner: nil,
			contains: []string{
				"Moved to dungeon/completed:",
				"docs/finished.md",
				"Moved to dungeon/someday:",
				"docs/maybe-later.md",
			},
		},
		{
			name:         "both nil summaries",
			campaignRoot: "/home/user/campaign",
			cwd:          "/home/user/campaign",
			triage:       nil,
			inner:        nil,
			contains:     []string{},
		},
		{
			name:         "no moves in summaries",
			campaignRoot: "/home/user/campaign",
			cwd:          "/home/user/campaign",
			triage: &dungeon.CrawlSummary{
				Kept:         3,
				StatusCounts: map[string]int{},
				MovedItems:   map[string][]string{},
			},
			inner:    nil,
			contains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildCrawlCommitMessage(tt.campaignRoot, tt.cwd, tt.triage, tt.inner)

			for _, substr := range tt.contains {
				if !strings.Contains(result, substr) {
					t.Errorf("result should contain %q, got:\n%s", substr, result)
				}
			}

			for _, substr := range tt.notContains {
				if strings.Contains(result, substr) {
					t.Errorf("result should NOT contain %q, got:\n%s", substr, result)
				}
			}
		})
	}
}

func TestBuildCrawlCommitMessage_SortedStatuses(t *testing.T) {
	triage := &dungeon.CrawlSummary{
		StatusCounts: map[string]int{"someday": 1, "archived": 1, "completed": 1},
		MovedItems: map[string][]string{
			"someday":   {"z.md"},
			"archived":  {"b.md"},
			"completed": {"a.md"},
		},
	}

	result := buildCrawlCommitMessage("/root", "/root/dir", triage, nil)

	// Verify alphabetical order: archived before completed before someday
	archivedIdx := strings.Index(result, "dungeon/archived")
	completedIdx := strings.Index(result, "dungeon/completed")
	somedayIdx := strings.Index(result, "dungeon/someday")

	if archivedIdx >= completedIdx || completedIdx >= somedayIdx {
		t.Errorf("statuses should be in alphabetical order, got:\n%s", result)
	}
}
