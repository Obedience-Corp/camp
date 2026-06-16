package dungeon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
)

func TestFinalizeCrawl_FlushesPendingRewritesOnSessionError(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dungeonPath := filepath.Join(root, "dungeon")
	svc := intdungeon.NewService(root, dungeonPath)
	if _, err := svc.Init(ctx, intdungeon.InitOptions{}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "item.md"), []byte("# Item\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	referrer := filepath.Join(root, "notes", "ref.md")
	if err := os.WriteFile(referrer, []byte("[i](../item.md)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc.BeginLinkBatch()
	dst, err := svc.MoveToDungeonStatus(ctx, "item.md", root, "completed")
	if err != nil {
		t.Fatalf("MoveToDungeonStatus: %v", err)
	}

	if got := readCrawlTestFile(t, referrer); got != "[i](../item.md)\n" {
		t.Fatalf("referrer rewritten before finalize: %q", got)
	}

	relDst, err := filepath.Rel(root, dst)
	if err != nil {
		t.Fatal(err)
	}
	triage := &intdungeon.CrawlSummary{MovedItems: map[string][]string{"completed": {filepath.ToSlash(relDst)}}}

	sessionErr := errors.New("forced inner crawl failure")
	gotErr := finalizeCrawl(ctx, nil, svc, triage, nil, false, sessionErr)

	if !errors.Is(gotErr, sessionErr) {
		t.Fatalf("finalizeCrawl error = %v, want it to surface the session error", gotErr)
	}
	want := "[i](../" + filepath.ToSlash(relDst) + ")\n"
	if got := readCrawlTestFile(t, referrer); got != want {
		t.Fatalf("finalizeCrawl must flush pending rewrites even on session error: referrer = %q, want %q", got, want)
	}
}

func readCrawlTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

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
					"completed": {"workflow/design/dungeon/completed/2026-03-15/old-feature", "workflow/design/dungeon/completed/2026-03-15/done-thing.md"},
				},
			},
			inner: nil,
			contains: []string{
				"Moved to dungeon/completed:",
				"workflow/design/old-feature",
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
					"archived": {"workflow/design/dungeon/archived/2026-03-15/deprecated.md"},
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
					"someday":   {"docs/dungeon/someday/2026-03-15/maybe-later.md"},
					"completed": {"docs/dungeon/completed/2026-03-15/finished.md"},
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
					"docs/architecture/api": {"docs/architecture/api/legacy-notes.md"},
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
			result := intdungeon.BuildCrawlCommitMessage(tt.campaignRoot, tt.cwd, tt.triage, tt.inner)

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
			"someday":   {"dir/dungeon/someday/2026-03-15/z.md"},
			"archived":  {"dir/dungeon/archived/2026-03-15/b.md"},
			"completed": {"dir/dungeon/completed/2026-03-15/a.md"},
		},
	}

	result := intdungeon.BuildCrawlCommitMessage("/root", "/root/dir", triage, nil)

	// Verify alphabetical order: archived before completed before someday
	archivedIdx := strings.Index(result, "dungeon/archived")
	completedIdx := strings.Index(result, "dungeon/completed")
	somedayIdx := strings.Index(result, "dungeon/someday")

	if archivedIdx >= completedIdx || completedIdx >= somedayIdx {
		t.Errorf("statuses should be in alphabetical order, got:\n%s", result)
	}
}

func TestCrawlCommitPaths(t *testing.T) {
	summary := &intdungeon.CrawlSummary{
		MovedItems: map[string][]string{
			"docs/api":  {"docs/api/routed.md"},
			"completed": {"workflow/design/dungeon/completed/2026-03-15/finished.md"},
		},
	}

	got := intdungeon.CrawlCommitPaths("workflow/design/dungeon", nil, summary)
	want := []string{
		"docs/api/routed.md",
		"workflow/design/dungeon/completed/2026-03-15/finished.md",
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
			"docs/api":  {"docs/api/routed.md"},
			"completed": {"workflow/design/dungeon/completed/2026-03-15/finished.md"},
		},
	}
	inner := &intdungeon.CrawlSummary{
		MovedItems: map[string][]string{
			"archived": {"workflow/design/dungeon/archived/2026-03-15/old-root.md"},
		},
	}

	got := intdungeon.CrawlSourceDeletionPaths(
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
