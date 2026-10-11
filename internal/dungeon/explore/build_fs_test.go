//go:build container_fs

package explore

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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

	finished, err := Apply(idx, Query{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for _, item := range finished.Items {
		if item.Status == StatusHolding {
			t.Fatalf("finished lens included holding item %s", item.Path)
		}
	}
	holding, err := Apply(idx, Query{Status: "holding"})
	if err != nil {
		t.Fatalf("Apply(holding) error = %v", err)
	}
	if len(holding.Items) != 1 || holding.Items[0].Path != "festivals/.dungeon/holding.md" {
		t.Fatalf("holding = %+v", holding.Items)
	}

	onlyFest, err := Apply(idx, Query{Dungeon: "Festivals", Text: "FA0024"})
	if err != nil {
		t.Fatalf("Apply(dungeon) error = %v", err)
	}
	if len(onlyFest.Items) != 1 || onlyFest.Items[0].ID != "FA0024" {
		t.Fatalf("filtered = %+v", onlyFest.Items)
	}
	if _, err := Apply(idx, Query{Status: "nope"}); err == nil {
		t.Fatal("unknown status error = nil")
	}
}

func TestBuildFestivalGoalSuppliesMissingName(t *testing.T) {
	root := t.TempDir()
	fromGoal := filepath.Join(root, "festivals", ".dungeon", "completed", "2026-09-15", "goal-named-FA0099")
	mustWrite(t, filepath.Join(fromGoal, "fest.yaml"), `
metadata:
  id: FA0099
  goal: Keep the fest.yaml goal
`)
	mustWrite(t, filepath.Join(fromGoal, "FESTIVAL_GOAL.md"), `---
fest_name: Goal Supplied Name
fest_id: IGNORE
---

# Goal

**Primary Goal:** This summary must not replace fest.yaml
`)

	explicit := filepath.Join(root, "festivals", ".dungeon", "completed", "2026-09-14", "explicit-name-FA0100")
	mustWrite(t, filepath.Join(explicit, "fest.yaml"), `
metadata:
  id: FA0100
  name: Explicit Name
  goal: Named in fest.yaml
`)
	mustWrite(t, filepath.Join(explicit, "FESTIVAL_GOAL.md"), `---
fest_name: Other Name
---

**Primary Goal:** Do not use this summary
`)

	basenamed := filepath.Join(root, "festivals", ".dungeon", "completed", "2026-09-13", "basename-only-FA0101")
	mustWrite(t, filepath.Join(basenamed, "fest.yaml"), `
metadata:
  id: FA0101
  goal: No festival name anywhere
`)
	mustWrite(t, filepath.Join(basenamed, "FESTIVAL_GOAL.md"), "# Goal\n\n**Primary Goal:** Still no name\n")

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	byPath := map[string]Item{}
	for _, item := range idx.Items {
		byPath[item.Path] = item
	}

	got := byPath["festivals/.dungeon/completed/2026-09-15/goal-named-FA0099"]
	if got.Title != "Goal Supplied Name" || got.ID != "FA0099" || got.Summary != "Keep the fest.yaml goal" {
		t.Fatalf("goal-named row = %+v", got)
	}
	found, err := Apply(idx, Query{Text: "goal supplied name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Items) != 1 || found.Items[0].ID != "FA0099" {
		t.Fatalf("title search = %+v", found.Items)
	}

	named := byPath["festivals/.dungeon/completed/2026-09-14/explicit-name-FA0100"]
	if named.Title != "Explicit Name" || named.Summary != "Named in fest.yaml" {
		t.Fatalf("explicit name row = %+v", named)
	}
	base := byPath["festivals/.dungeon/completed/2026-09-13/basename-only-FA0101"]
	if base.Title != "basename-only-FA0101" || base.ID != "FA0101" || base.Summary != "No festival name anywhere" {
		t.Fatalf("basename row = %+v", base)
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

func TestBuildSanitizesRepositoryText(t *testing.T) {
	root := t.TempDir()
	rel := "festivals/.dungeon/completed/2026-10-05/evil-FA0099"
	mustWrite(t, filepath.Join(root, filepath.FromSlash(rel), "fest.yaml"),
		"metadata:\n  id: \"FA\\u009b01\"\n  name: \"evil\\e]52;c;ZXZpbA==\\aname\"\n  goal: \"Goal\\u0085 with \\e[31mred\\e[0m text\"\n")
	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	var item Item
	for _, candidate := range idx.Items {
		if candidate.Path == rel {
			item = candidate
		}
	}
	if item.ID != "FA01" || item.Title != "evilname" || item.Summary != "Goal with red text" {
		t.Fatalf("item = id %q title %q summary %q", item.ID, item.Title, item.Summary)
	}
	encoded, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"\u009b", "\u0085", `\u001b`, `\u0007`} {
		if strings.Contains(string(encoded), raw) {
			t.Fatalf("index JSON carries %q: %s", raw, encoded)
		}
	}
}

func TestBuildContinuesPastUnreadableDirectory(t *testing.T) {
	root := t.TempDir()
	const rel = "festivals/.dungeon/completed/note.md"
	mustWrite(t, filepath.Join(root, filepath.FromSlash(rel)), "# Note\n\nShipped the note.\n")
	if err := os.MkdirAll(filepath.Join(root, "blocked", "hidden"), 0o755); err != nil {
		t.Fatal(err)
	}

	orig := walkDir
	walkDir = func(walkRoot string, fn fs.WalkDirFunc) error {
		return orig(walkRoot, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry != nil && entry.IsDir() && entry.Name() == "blocked" {
				if callErr := fn(path, entry, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, entry, os.ErrPermission); callErr != nil {
					return callErr
				}
				return fs.SkipDir
			}
			return fn(path, entry, err)
		})
	}
	t.Cleanup(func() { walkDir = orig })

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if itemAt(t, idx, rel).Status != "completed" {
		t.Fatal("readable dungeon was not indexed")
	}
	if len(idx.Warnings) != 1 || idx.Warnings[0] != "blocked: permission denied" {
		t.Fatalf("warnings = %#v", idx.Warnings)
	}

	cached := idx
	cached.Warnings = nil
	refreshed, _, err := Refresh(context.Background(), root, cached)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if itemAt(t, refreshed, rel).Status != "completed" {
		t.Fatal("Refresh dropped the readable dungeon")
	}
	if len(refreshed.Warnings) != 1 || refreshed.Warnings[0] != "blocked: permission denied" {
		t.Fatalf("refresh warnings = %#v", refreshed.Warnings)
	}
}

func TestBuildKeepsReadableItemsWhenFingerprintFails(t *testing.T) {
	root := t.TempDir()
	const completed = "festivals/.dungeon/completed/note.md"
	const archived = "festivals/.dungeon/archived/old.md"
	mustWrite(t, filepath.Join(root, filepath.FromSlash(completed)), "# Note\n\nShipped the note.\n")
	mustWrite(t, filepath.Join(root, filepath.FromSlash(archived)), "# Old\n\nArchived note.\n")

	orig := walkDir
	walkDir = func(walkRoot string, fn fs.WalkDirFunc) error {
		fingerprintWalk := filepath.Base(walkRoot) == ".dungeon"
		return orig(walkRoot, func(path string, entry fs.DirEntry, err error) error {
			if fingerprintWalk && err == nil && entry != nil && entry.IsDir() && entry.Name() == "archived" {
				return fn(path, entry, os.ErrPermission)
			}
			return fn(path, entry, err)
		})
	}
	t.Cleanup(func() { walkDir = orig })

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if itemAt(t, idx, completed).Status != "completed" {
		t.Fatal("readable completed item was dropped")
	}
	if itemAt(t, idx, archived).Status != "archived" {
		t.Fatal("readable archived item was dropped")
	}
	if len(idx.Dungeons) != 1 || idx.Dungeons[0].Fingerprint != "" {
		t.Fatalf("dungeons = %+v, want one dungeon with an empty fingerprint", idx.Dungeons)
	}
	if len(idx.Warnings) != 1 || idx.Warnings[0] != "festivals/.dungeon: permission denied" {
		t.Fatalf("warnings = %#v", idx.Warnings)
	}

	walkDir = orig
	cleared := idx
	cleared.Warnings = nil
	refreshed, changed, err := Refresh(context.Background(), root, cleared)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !changed || refreshed.Dungeons[0].Fingerprint == "" {
		t.Fatal("Refresh accepted the incomplete fingerprint")
	}
	if itemAt(t, refreshed, completed).Status != "completed" {
		t.Fatal("Refresh dropped the readable completed item")
	}
}

func TestBuildWorkitemIDWithoutRef(t *testing.T) {
	root := t.TempDir()
	legacy := "workflow/design/.dungeon/completed/legacy-notes"
	mustWrite(t, filepath.Join(root, filepath.FromSlash(legacy), ".workitem"), `version: v1alpha4
kind: workitem
id: design-legacy-checklist
type: design
title: Legacy checklist
`)
	both := "workflow/design/.dungeon/completed/current-notes"
	mustWrite(t, filepath.Join(root, filepath.FromSlash(both), ".workitem"), `version: v1alpha9
kind: workitem
id: design-current-checklist
type: design
title: Current checklist
ref: WI-abc123
`)

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := itemAt(t, idx, legacy); got.ID != "design-legacy-checklist" || got.Kind != KindWorkitem {
		t.Fatalf("legacy row = %+v", got)
	}
	found, err := Apply(idx, Query{Text: "design-legacy-checklist"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Items) != 1 || found.Items[0].Path != legacy {
		t.Fatalf("query by marker id = %+v", found.Items)
	}

	if got := itemAt(t, idx, both); got.ID != "WI-abc123" {
		t.Fatalf("id = %q, want the ref", got.ID)
	}
	byRef, err := Apply(idx, Query{Text: "WI-abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byRef.Items) != 1 || byRef.Items[0].Path != both {
		t.Fatalf("query by ref = %+v", byRef.Items)
	}
	byMarkerID, err := Apply(idx, Query{Text: "design-current-checklist"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byMarkerID.Items) != 0 {
		t.Fatalf("marker id replaced the ref: %+v", byMarkerID.Items)
	}
}

// Each metadata file is read up to 256 KB. Without a bound on what a row
// keeps, 200 such files put 50 MB in the JSON and in the cache file.
func TestBuildBoundsHostileMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	done := filepath.Join(root, ".dungeon", "done")
	const files = 200
	body := "---\ntitle: " + strings.Repeat("A", 200_000) + "\nid: " + strings.Repeat("B", 40_000) + "\n---\n\n" + strings.Repeat("C", 4_000) + "\n"
	for i := range files {
		mustWrite(t, filepath.Join(done, fmt.Sprintf("n%03d.md", i)), body)
	}
	// Every line repeats one 1,000-character key, and the decoder quotes it in
	// each complaint.
	repeated := strings.Repeat("k"+strings.Repeat("a", 1_000)+": v\n", 200)
	mustWrite(t, filepath.Join(done, "noisy-festival", "fest.yaml"), repeated)

	idx, err := Build(ctx, root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(idx.Items) != files+1 {
		t.Fatalf("indexed %d rows, want %d", len(idx.Items), files+1)
	}
	for _, item := range idx.Items {
		for field, got := range map[string]struct {
			text  string
			limit int
		}{
			"title":   {item.Title, titleLimit},
			"id":      {item.ID, idLimit},
			"summary": {item.Summary, summaryLimit},
			"warning": {item.Warning, warningLimit},
		} {
			if n := utf8.RuneCountInString(got.text); n > got.limit {
				t.Fatalf("%s: %s holds %d runes, limit %d", item.Path, field, n, got.limit)
			}
		}
	}
	first := itemAt(t, idx, ".dungeon/done/n000.md")
	if first.Title != strings.Repeat("A", titleLimit) || first.ID != strings.Repeat("B", idLimit) || first.Summary != strings.Repeat("C", summaryLimit) {
		t.Fatalf("oversized row was not cut to its limits: title %d, id %d, summary %d", len(first.Title), len(first.ID), len(first.Summary))
	}
	noisy := itemAt(t, idx, ".dungeon/done/noisy-festival")
	if utf8.RuneCountInString(noisy.Warning) != warningLimit || !strings.HasPrefix(noisy.Warning, "fest.yaml: ") {
		t.Fatalf("noisy warning holds %d runes: %.60q", utf8.RuneCountInString(noisy.Warning), noisy.Warning)
	}

	// 4 KB a row is generous for the limits above and 60 times under what the
	// files hold.
	const bound = (files + 1) * 4096
	payload, err := json.Marshal(idx.Items)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > bound {
		t.Fatalf("items encode to %d bytes of JSON, want at most %d", len(payload), bound)
	}
	cacheDir := filepath.Join(t.TempDir(), "dungeon-feed")
	if err := SaveCache(ctx, cacheDir, idx); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(cacheDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > bound {
		t.Fatalf("cache file is %d bytes, want at most %d", info.Size(), bound)
	}
}
