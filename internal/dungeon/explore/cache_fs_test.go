//go:build container_fs

package explore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stageExploreCamp(t *testing.T) (root, festRel, workRel string) {
	t.Helper()
	root = t.TempDir()
	festRel = "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024"
	fest := filepath.Join(root, filepath.FromSlash(festRel))
	mustWrite(t, filepath.Join(fest, "fest.yaml"), "metadata:\n  id: FA0024\n  name: festival-activity-desktop\n  goal: Ship it\n")
	mustWrite(t, filepath.Join(fest, "FESTIVAL_GOAL.md"), "# goal\n")
	workRel = "workflow/design/.dungeon/completed/2026-10-03/fresh-checklist"
	work := filepath.Join(root, filepath.FromSlash(workRel))
	mustWrite(t, filepath.Join(work, ".workitem"), "title: Camp fresh checklist\nref: WI-abc\n")
	mustWrite(t, filepath.Join(work, "README.md"), "# Checklist\n\nAlign camp fresh and the sweep.\n")
	mustWrite(t, filepath.Join(root, ".campaign", "intents", ".dungeon", "done", "add-blog.md"), "# Add blog\n\nStand up a blog.\n")
	return root, festRel, workRel
}

func itemAt(t *testing.T, idx Index, rel string) Item {
	t.Helper()
	for _, item := range idx.Items {
		if item.Path == rel {
			return item
		}
	}
	t.Fatalf("no item at %s in %+v", rel, idx.Items)
	return Item{}
}

func TestCacheKeepsDirectoryItems(t *testing.T) {
	ctx := context.Background()
	root, festRel, workRel := stageExploreCamp(t)
	built, err := Build(ctx, root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	cacheDir := filepath.Join(t.TempDir(), "dungeon-feed")
	if err := SaveCache(cacheDir, built); err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}
	loaded, ok, err := LoadCache(cacheDir)
	if err != nil || !ok {
		t.Fatalf("LoadCache() = ok %v, err %v", ok, err)
	}
	refreshed, changed, err := Refresh(ctx, root, loaded)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if changed {
		t.Fatal("Refresh() rebuilt although nothing changed on disk")
	}
	for _, idx := range []Index{loaded, refreshed} {
		if !itemAt(t, idx, festRel).IsDir {
			t.Errorf("festival %s reloaded as a file", festRel)
		}
		if !itemAt(t, idx, workRel).IsDir {
			t.Errorf("workitem %s reloaded as a file", workRel)
		}
		if itemAt(t, idx, ".campaign/intents/.dungeon/done/add-blog.md").IsDir {
			t.Error("markdown file reloaded as a directory")
		}
	}
}

func TestRefreshSeesInPlaceMetadataEdits(t *testing.T) {
	cases := []struct {
		name  string
		file  func(festRel, workRel string) string
		body  string
		check func(t *testing.T, idx Index, festRel, workRel string)
	}{
		{
			name: "fest.yaml",
			file: func(festRel, _ string) string { return festRel + "/fest.yaml" },
			body: "metadata:\n  id: FA0024\n  name: renamed-festival\n  goal: Ship it\n",
			check: func(t *testing.T, idx Index, festRel, _ string) {
				if got := itemAt(t, idx, festRel).Title; got != "renamed-festival" {
					t.Errorf("festival title = %q, want renamed-festival", got)
				}
			},
		},
		{
			name: ".workitem",
			file: func(_, workRel string) string { return workRel + "/.workitem" },
			body: "title: Renamed checklist\nref: WI-abc\n",
			check: func(t *testing.T, idx Index, _, workRel string) {
				if got := itemAt(t, idx, workRel).Title; got != "Renamed checklist" {
					t.Errorf("workitem title = %q, want Renamed checklist", got)
				}
			},
		},
		{
			name: "README.md",
			file: func(_, workRel string) string { return workRel + "/README.md" },
			body: "# Checklist\n\nA rewritten summary.\n",
			check: func(t *testing.T, idx Index, _, workRel string) {
				if got := itemAt(t, idx, workRel).Summary; got != "A rewritten summary." {
					t.Errorf("workitem summary = %q, want A rewritten summary.", got)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root, festRel, workRel := stageExploreCamp(t)
			cached, err := Build(ctx, root)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			path := filepath.Join(root, filepath.FromSlash(tc.file(festRel, workRel)))
			dir := filepath.Dir(path)
			dirInfo, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			later := time.Now().Add(2 * time.Second)
			if err := os.Chtimes(path, later, later); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(dir, dirInfo.ModTime(), dirInfo.ModTime()); err != nil {
				t.Fatal(err)
			}
			refreshed, changed, err := Refresh(ctx, root, cached)
			if err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if !changed {
				t.Fatalf("Refresh() kept the cache after %s was edited in place", tc.name)
			}
			tc.check(t, refreshed, festRel, workRel)
		})
	}
}

func TestWorkitemMarkerWinsOverFestivalArtifacts(t *testing.T) {
	root := t.TempDir()
	rel := "festivals/.dungeon/completed/2026-10-06/resident"
	dir := filepath.Join(root, filepath.FromSlash(rel))
	mustWrite(t, filepath.Join(dir, ".workitem"), "title: Resident design\nref: WI-123\n")
	mustWrite(t, filepath.Join(dir, "README.md"), "# Resident\n\nThe design that took over this folder.\n")
	mustWrite(t, filepath.Join(dir, "fest.yaml"), "metadata:\n  id: SF0001\n  name: stale-festival\n  goal: Stale goal\n")
	mustWrite(t, filepath.Join(dir, "FESTIVAL_GOAL.md"), "# goal\n")
	mustWrite(t, filepath.Join(dir, "festival-replay.gif"), "gif")

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	item := itemAt(t, idx, rel)
	if item.Kind != KindWorkitem {
		t.Fatalf("kind = %q, want %q (the .workitem marker wins)", item.Kind, KindWorkitem)
	}
	if item.Title != "Resident design" || item.ID != "WI-123" {
		t.Fatalf("title/id = %q/%q, want the workitem marker's", item.Title, item.ID)
	}
	if item.Summary != "The design that took over this folder." {
		t.Fatalf("summary = %q, want the workitem README", item.Summary)
	}
	if item.Replay != "" {
		t.Fatalf("replay = %q, want none for a workitem", item.Replay)
	}
}

func TestCachePathConfinement(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".obey", "campaign", "caches")
	for _, id := range []string{"normal", "../../../../checkout", "/absolute", "", `..\escape`} {
		dir, err := CacheDir(id, "/camp")
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(base, dir)
		if err != nil || !filepath.IsLocal(rel) {
			t.Fatalf("cache escaped for %q: %s", id, dir)
		}
		if err := SaveCache(dir, Index{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCacheWriteSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "private")
	mustWrite(t, target, "untouched")
	for _, name := range []string{"index.json.tmp", "index.json"} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveCache(dir, Index{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("cache write followed symlink: %q, %v", data, err)
	}
	if _, ok, err := LoadCache(dir); err != nil || !ok {
		t.Fatalf("cache not published: %v %v", ok, err)
	}
}
