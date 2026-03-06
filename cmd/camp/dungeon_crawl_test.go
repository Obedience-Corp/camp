package main

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/dungeon"
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
			name:         "docs destination move formatting",
			campaignRoot: "/home/user/campaign",
			cwd:          "/home/user/campaign/workflow/design",
			triage: &dungeon.CrawlSummary{
				StatusCounts: map[string]int{"docs/architecture/api": 1},
				MovedItems: map[string][]string{
					"docs/architecture/api": {"legacy-notes.md"},
				},
			},
			inner: nil,
			contains: []string{
				"Moved to docs/architecture/api:",
				"workflow/design/legacy-notes.md",
			},
			notContains: []string{
				"Moved to dungeon/docs/architecture/api:",
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

func TestCrawlDocsDestinationPaths(t *testing.T) {
	tests := []struct {
		name    string
		summary *dungeon.CrawlSummary
		want    []string
	}{
		{
			name: "nil summary",
		},
		{
			name: "no docs destinations",
			summary: &dungeon.CrawlSummary{
				MovedItems: map[string][]string{
					"archived": {"old.md"},
				},
			},
		},
		{
			name: "collect docs destinations sorted and unique",
			summary: &dungeon.CrawlSummary{
				MovedItems: map[string][]string{
					"docs/api":          {"a.md"},
					"docs/guides/setup": {"b.md"},
					"completed":         {"c.md"},
				},
			},
			want: []string{"docs/api", "docs/guides/setup"},
		},
		{
			name: "drop unsafe docs paths",
			summary: &dungeon.CrawlSummary{
				MovedItems: map[string][]string{
					"docs/../escape": {"a.md"},
					"docs/safe":      {"b.md"},
				},
			},
			want: []string{"docs/safe"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := crawlDocsDestinationPaths(tt.summary)
			if len(got) != len(tt.want) {
				t.Fatalf("crawlDocsDestinationPaths() len=%d, want=%d (%v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("crawlDocsDestinationPaths()[%d]=%q, want=%q (full=%v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}
