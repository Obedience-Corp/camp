package dungeon

import (
	"strings"
	"testing"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
)

func TestBuildCrawlCommitMessage(t *testing.T) {
	tests := []struct {
		name         string
		campaignRoot string
		cwd          string
		triage       *intdungeon.CrawlSummary
		inner        *intdungeon.CrawlSummary
		contains     []string
		notContains  []string
	}{
		{
			name:         "triage only",
			campaignRoot: "/home/user/campaign",
			cwd:          "/home/user/campaign/workflow/design",
			triage: &intdungeon.CrawlSummary{
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
			inner: &intdungeon.CrawlSummary{
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
			triage: &intdungeon.CrawlSummary{
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
			triage: &intdungeon.CrawlSummary{
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
			triage: &intdungeon.CrawlSummary{
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
	triage := &intdungeon.CrawlSummary{
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

func TestCrawlMovedItemPaths(t *testing.T) {
	tests := []struct {
		name    string
		dungeon string
		summary *intdungeon.CrawlSummary
		want    []string
	}{
		{
			name:    "nil summary",
			dungeon: "workflow/design/dungeon",
		},
		{
			name:    "no moves",
			dungeon: "workflow/design/dungeon",
			summary: &intdungeon.CrawlSummary{
				MovedItems: map[string][]string{
					"archived": {"old.md"},
				},
			},
			want: []string{"workflow/design/dungeon/archived/old.md"},
		},
		{
			name:    "collect docs and dungeon destinations sorted and unique",
			dungeon: "workflow/design/dungeon",
			summary: &intdungeon.CrawlSummary{
				MovedItems: map[string][]string{
					"docs/api":          {"a.md", "a.md"},
					"docs/guides/setup": {"b.md"},
					"completed":         {"c.md"},
				},
			},
			want: []string{
				"docs/api/a.md",
				"docs/guides/setup/b.md",
				"workflow/design/dungeon/completed/c.md",
			},
		},
		{
			name:    "drop unsafe paths",
			dungeon: "workflow/design/dungeon",
			summary: &intdungeon.CrawlSummary{
				MovedItems: map[string][]string{
					"docs/../escape": {"a.md"},
					"docs/safe":      {"b.md"},
					"completed":      {"../bad.md", "good.md"},
				},
			},
			want: []string{
				"docs/safe/b.md",
				"workflow/design/dungeon/completed/good.md",
			},
		},
		{
			name:    "support direct docs root destination",
			dungeon: "workflow/design/dungeon",
			summary: &intdungeon.CrawlSummary{
				MovedItems: map[string][]string{
					"docs": {"top-level.md"},
				},
			},
			want: []string{"docs/top-level.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := crawlMovedItemPaths(tt.dungeon, tt.summary)
			if len(got) != len(tt.want) {
				t.Fatalf("crawlMovedItemPaths() len=%d, want=%d (%v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("crawlMovedItemPaths()[%d]=%q, want=%q (full=%v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestCrawlCommitPaths(t *testing.T) {
	summary := &intdungeon.CrawlSummary{
		MovedItems: map[string][]string{
			"docs/api":  {"routed.md"},
			"completed": {"finished.md"},
		},
	}

	got := crawlCommitPaths("workflow/design/dungeon", summary)
	want := []string{
		"docs/api/routed.md",
		"workflow/design/dungeon/completed/finished.md",
		"workflow/design/dungeon/crawl.jsonl",
	}

	if len(got) != len(want) {
		t.Fatalf("crawlCommitPaths() len=%d, want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("crawlCommitPaths()[%d]=%q, want=%q (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestCrawlSourceDeletionPaths(t *testing.T) {
	summary := &intdungeon.CrawlSummary{
		MovedItems: map[string][]string{
			"docs/api":  {"routed.md"},
			"completed": {"finished.md"},
		},
	}
	inner := &intdungeon.CrawlSummary{
		MovedItems: map[string][]string{
			"archived": {"old-root.md"},
		},
	}

	got := crawlSourceDeletionPaths(
		"/home/user/campaign",
		"/home/user/campaign/workflow/design",
		"workflow/design/dungeon",
		summary,
		inner,
	)

	want := []string{
		"workflow/design/dungeon/old-root.md",
		"workflow/design/finished.md",
		"workflow/design/routed.md",
	}
	if len(got) != len(want) {
		t.Fatalf("crawlSourceDeletionPaths() len=%d, want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("crawlSourceDeletionPaths()[%d]=%q, want=%q (full=%v)", i, got[i], want[i], got)
		}
	}
}
