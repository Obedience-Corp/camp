package explore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildFeed(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "projects", "camp", "internal", "dungeon", "skip.md"), "nope\n")

	festDir := filepath.Join(root, "festivals", ".dungeon", "completed", "2026-10-05", "festival-activity-desktop-FA0024")
	mustWrite(t, filepath.Join(festDir, "fest.yaml"), `
metadata:
  id: FA0024
  name: festival-activity-desktop
  goal: Ship Festival Activity as a signed desktop app
  status_history:
    - status: dungeon/completed
      timestamp: 2026-09-01T00:00:00Z
`)
	mustWrite(t, filepath.Join(festDir, "FESTIVAL_GOAL.md"), "# goal\n")
	mustWrite(t, filepath.Join(festDir, "festival-replay.gif"), "gif")

	named := filepath.Join(root, "festivals", ".dungeon", "completed", "2026-10-01", "grok-provider-GP0001")
	mustWrite(t, filepath.Join(named, "fest.yaml"), `
metadata:
  id: GP0001
  name: grok-provider
  goal: Add the Grok provider
`)
	mustWrite(t, filepath.Join(named, "grok-provider-GP0001.gif"), "gif")

	flat := filepath.Join(root, "festivals", ".dungeon", "completed", "legacy-flat-FA0001")
	mustWrite(t, filepath.Join(flat, "fest.yaml"), `
metadata:
  id: FA0001
  name: legacy-flat
  goal: Flat layout festival
  status_history:
    - status: dungeon/completed
      timestamp: 2026-09-01T16:30:18-06:00
`)

	mustWrite(t, filepath.Join(root, ".campaign", "intents", ".dungeon", "done", "add-blog.md"), `---
id: add-blog
title: Add blog sections
status: dungeon/done
updated_at: 2026-08-21T06:04:49Z
---

# Add blog sections

Stand up a blog on both domains.
`)

	design := filepath.Join(root, "workflow", "design", ".dungeon", "completed", "2026-10-03", "fresh-checklist")
	mustWrite(t, filepath.Join(design, ".workitem"), "title: Camp fresh checklist\nref: WI-abc\n")
	mustWrite(t, filepath.Join(design, "README.md"), "# Checklist\n\nAlign camp fresh and the sweep.\n")

	custom := filepath.Join(root, "notes", ".dungeon", "completed", "2026-10-04", "scratch")
	mustWrite(t, filepath.Join(custom, "README.md"), "# Scratch\n\nA custom dungeon note.\n")

	mustWrite(t, filepath.Join(root, "festivals", ".dungeon", "holding.md"), "# Holding\n\nNot finished.\n")
	mustWrite(t, filepath.Join(root, "festivals", ".dungeon", "OBEY.md"), "# skip\n")

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	byPath := map[string]Item{}
	for _, item := range idx.Items {
		byPath[item.Path] = item
	}
	if _, ok := byPath["projects/camp/internal/dungeon/skip.md"]; ok {
		t.Fatal("projects/ dungeon content was indexed")
	}

	fest := byPath["festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024"]
	if fest.ID != "FA0024" || fest.Kind != KindFestival || fest.DoneDate != "2026-10-05" || fest.DateSource != DateBucket {
		t.Fatalf("festival row = %+v", fest)
	}
	if fest.DungeonLabel != "Festivals" {
		t.Fatalf("label = %q", fest.DungeonLabel)
	}
	if fest.Replay != fest.Path+"/festival-replay.gif" {
		t.Fatalf("replay = %q", fest.Replay)
	}
	if fest.Summary != "Ship Festival Activity as a signed desktop app" {
		t.Fatalf("summary = %q", fest.Summary)
	}

	namedItem := byPath["festivals/.dungeon/completed/2026-10-01/grok-provider-GP0001"]
	if namedItem.Replay != namedItem.Path+"/grok-provider-GP0001.gif" {
		t.Fatalf("named replay = %q", namedItem.Replay)
	}

	legacy := byPath["festivals/.dungeon/completed/legacy-flat-FA0001"]
	if legacy.DoneDate != "2026-09-01" || legacy.DateSource != DateHistory {
		t.Fatalf("legacy row = %+v", legacy)
	}

	intent := byPath[".campaign/intents/.dungeon/done/add-blog.md"]
	if intent.Kind != KindMarkdown || intent.Title != "Add blog sections" || intent.DoneDate != "2026-08-21" || intent.DateSource != DateHistory {
		t.Fatalf("intent row = %+v", intent)
	}
	if intent.DungeonLabel != "Intents" {
		t.Fatalf("intent label = %q", intent.DungeonLabel)
	}

	work := byPath["workflow/design/.dungeon/completed/2026-10-03/fresh-checklist"]
	if work.Kind != KindWorkitem || work.ID != "WI-abc" || work.DungeonLabel != "Designs" {
		t.Fatalf("workitem row = %+v", work)
	}

	customItem := byPath["notes/.dungeon/completed/2026-10-04/scratch"]
	if customItem.DungeonLabel != "notes" {
		t.Fatalf("custom label = %q", customItem.DungeonLabel)
	}

	if idx.Items[0].DoneDate < "2026-10-05" {
		t.Fatalf("newest first = %s", idx.Items[0].DoneDate)
	}

	finished, err := Apply(idx.Items, Query{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for _, item := range finished.Items {
		if item.Status == StatusHolding {
			t.Fatalf("finished lens included holding item %s", item.Path)
		}
	}
	holding, err := Apply(idx.Items, Query{Status: "holding"})
	if err != nil {
		t.Fatalf("Apply(holding) error = %v", err)
	}
	if len(holding.Items) != 1 || holding.Items[0].Path != "festivals/.dungeon/holding.md" {
		t.Fatalf("holding = %+v", holding.Items)
	}

	onlyFest, err := Apply(idx.Items, Query{Dungeon: "Festivals", Text: "FA0024"})
	if err != nil {
		t.Fatalf("Apply(dungeon) error = %v", err)
	}
	if len(onlyFest.Items) != 1 || onlyFest.Items[0].ID != "FA0024" {
		t.Fatalf("filtered = %+v", onlyFest.Items)
	}
	if _, err := Apply(idx.Items, Query{Status: "nope"}); err == nil {
		t.Fatal("unknown status error = nil")
	}
}

func TestApplySinceUntil(t *testing.T) {
	items := []Item{
		{DoneDate: "2026-10-01", Title: "a", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "completed"},
		{DoneDate: "2026-10-05", Title: "b", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "completed"},
	}
	got, err := Apply(items, Query{Since: "2026-10-05", Until: "2026-10-05"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Title != "b" {
		t.Fatalf("items = %+v", got.Items)
	}
	if _, err := Apply(items, Query{Since: "2026-10-06", Until: "2026-10-01"}); err == nil {
		t.Fatal("since after until error = nil")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
