//go:build container_fs

package explore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if err := SaveCache(ctx, cacheDir, built); err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}
	loaded, ok, err := LoadCache(ctx, cacheDir)
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

func TestRefreshRetriesCachedMetadataFailure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	festRel := "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024"
	fest := filepath.Join(root, filepath.FromSlash(festRel))
	good := "metadata:\n  id: FA0024\n  name: festival-activity-desktop\n  goal: Ship it\n"
	bad := strings.Replace(good, "metadata:", "metadata[", 1)
	if len(bad) != len(good) {
		t.Fatalf("bad yaml len = %d, good len = %d", len(bad), len(good))
	}
	yamlPath := filepath.Join(fest, "fest.yaml")
	mustWrite(t, yamlPath, bad)

	cached, err := Build(ctx, root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	incomplete := itemAt(t, cached, festRel)
	if len(cached.Warnings) != 0 || incomplete.Warning == "" || incomplete.Title == "festival-activity-desktop" {
		t.Fatalf("cached item = %+v, warnings = %#v", incomplete, cached.Warnings)
	}

	paths := []string{
		yamlPath,
		fest,
		filepath.Dir(fest),
		filepath.Dir(filepath.Dir(fest)),
		filepath.Dir(filepath.Dir(filepath.Dir(fest))),
	}
	mods := make([]time.Time, len(paths))
	for i, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		mods[i] = info.ModTime()
	}
	if err := os.WriteFile(yamlPath, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths {
		if err := os.Chtimes(path, mods[i], mods[i]); err != nil {
			t.Fatal(err)
		}
	}
	fp, err := fingerprint(ctx, filepath.Join(root, "festivals", ".dungeon"))
	if err != nil {
		t.Fatal(err)
	}
	if fp != cached.Dungeons[0].Fingerprint {
		t.Fatalf("restored file changed the fingerprint: %s vs %s", fp, cached.Dungeons[0].Fingerprint)
	}

	refreshed, changed, err := Refresh(ctx, root, cached)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !changed {
		t.Fatal("Refresh kept the incomplete cached item")
	}
	got := itemAt(t, refreshed, festRel)
	if got.Warning != "" || got.Title != "festival-activity-desktop" || got.ID != "FA0024" || got.Summary != "Ship it" {
		t.Fatalf("retried item = %+v", got)
	}

	again, changed, err := Refresh(ctx, root, refreshed)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if changed {
		t.Fatal("warning-free cache was rebuilt")
	}
	warned := again
	warned.Warnings = []string{"festivals/.dungeon: permission denied"}
	if _, changed, err = Refresh(ctx, root, warned); err != nil || !changed {
		t.Fatalf("index warning refresh changed=%v err=%v", changed, err)
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
	t.Setenv("XDG_CONFIG_HOME", "")
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
		if err := SaveCache(context.Background(), dir, Index{}); err != nil {
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
	if err := SaveCache(context.Background(), dir, Index{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("cache write followed symlink: %q, %v", data, err)
	}
	if _, ok, err := LoadCache(context.Background(), dir); err != nil || !ok {
		t.Fatalf("cache not published: %v %v", ok, err)
	}
}

func TestCacheDiscardsEarlierSchemas(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := SaveCache(ctx, dir, Index{Items: []Item{{Title: "current"}}}); err != nil {
		t.Fatal(err)
	}
	if cached, ok, err := LoadCache(ctx, dir); err != nil || !ok || len(cached.Items) != 1 {
		t.Fatalf("current cache: ok %v, err %v, items %d", ok, err, len(cached.Items))
	}
	// The zone is the current one, so only the schema can turn these away.
	for _, version := range []string{"v1", "v2", "v3", "v4", "v5", "v6", "v7"} {
		stale := `{"schema_version":"camp-dungeon-explore-cache/` + version + `","zone":"` + zoneKey() + `","index":{"items":[{"title":"stale"}],"dungeons":[]}}`
		mustWrite(t, filepath.Join(dir, "index.json"), stale)
		if cached, ok, err := LoadCache(ctx, dir); err != nil || ok {
			t.Fatalf("cache written by schema %s was trusted: %+v (err %v)", version, cached, err)
		}
	}
}

func TestCacheHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := filepath.Join(t.TempDir(), "dungeon-feed")
	if err := SaveCache(ctx, dir, Index{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, ok, err := LoadCache(ctx, dir); !errors.Is(err, context.Canceled) || ok {
		t.Fatalf("LoadCache with a cancelled context = ok %v, err %v", ok, err)
	}
	fresh := filepath.Join(t.TempDir(), "dungeon-feed")
	if err := SaveCache(ctx, fresh, Index{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("SaveCache with a cancelled context = %v", err)
	}
	if _, err := os.Lstat(fresh); !os.IsNotExist(err) {
		t.Fatalf("a cancelled save still created %s (%v)", fresh, err)
	}
}

// A chmod changes neither a file's size nor its modification time. The mode
// is in the fingerprint so the cached row is read again.
func TestRefreshSeesPermissionChange(t *testing.T) {
	ctx := context.Background()
	root, festRel, _ := stageExploreCamp(t)
	fest := filepath.Join(root, filepath.FromSlash(festRel))
	yamlPath := filepath.Join(fest, "fest.yaml")
	cached, err := Build(ctx, root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, changed, err := Refresh(ctx, root, cached); err != nil || changed {
		t.Fatalf("untouched camp: changed = %v, err = %v", changed, err)
	}

	for name, path := range map[string]string{"metadata file": yamlPath, "item directory": fest} {
		t.Run(name, func(t *testing.T) {
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			// Owner access stays, so the row reads the same with or without root.
			if err := os.Chmod(path, before.Mode().Perm()&^0o077|0o001); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, before.Mode().Perm()) })
			after, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if after.Mode() == before.Mode() {
				t.Fatalf("chmod left the mode at %v", before.Mode())
			}
			if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
				t.Fatalf("chmod moved the time or size: %v %d, then %v %d", before.ModTime(), before.Size(), after.ModTime(), after.Size())
			}
			refreshed, changed, err := Refresh(ctx, root, cached)
			if err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if !changed {
				t.Fatal("Refresh kept the cache across a permission change")
			}
			if got := itemAt(t, refreshed, festRel); got.ID != "FA0024" {
				t.Fatalf("rebuilt row = %+v", got)
			}
			if err := os.Chmod(path, before.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if _, changed, err := Refresh(ctx, root, cached); err != nil || changed {
				t.Fatalf("restored mode: changed = %v, err = %v", changed, err)
			}
		})
	}
}

// A save whose process exits partway cannot remove its temp file. The next
// save does, and leaves a save still in progress alone.
func TestSaveCacheSweepsStaleTempFiles(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "dungeon-feed")
	stale := filepath.Join(dir, ".index-1111.tmp")
	inProgress := filepath.Join(dir, ".index-2222.tmp")
	unrelated := filepath.Join(dir, "notes.tmp")
	for _, path := range []string{stale, inProgress, unrelated} {
		mustWrite(t, path, "{")
	}
	staleDir := filepath.Join(dir, ".index-3333.tmp")
	if err := os.Mkdir(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * staleTempAge)
	for _, path := range []string{stale, unrelated, staleDir} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	if err := SaveCache(ctx, dir, Index{Items: []Item{{Title: "current"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temp file survived the save: %v", err)
	}
	for _, path := range []string{inProgress, unrelated, staleDir} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("%s was removed: %v", filepath.Base(path), err)
		}
	}
	if cached, ok, err := LoadCache(ctx, dir); err != nil || !ok || len(cached.Items) != 1 {
		t.Fatalf("saved cache: ok %v, err %v, items %d", ok, err, len(cached.Items))
	}

	// Cancelled after the file is written and before it is published.
	fresh := filepath.Join(t.TempDir(), "dungeon-feed")
	cancelled := &cancelAfter{Context: ctx, after: 2}
	if err := SaveCache(cancelled, fresh, Index{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("SaveCache cancelled partway = %v", err)
	}
	left, err := os.ReadDir(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("a cancelled save left %d files behind, first %s", len(left), left[0].Name())
	}
}

// A row dated by its file time holds the calendar day of the zone it was
// read in. The same instant is another day elsewhere, and nothing on disk
// changes when the viewer does, so the cache is good for one zone.
func TestCacheIsReadAgainInAnotherTimeZone(t *testing.T) {
	ctx := context.Background()
	local := time.Local
	t.Cleanup(func() { time.Local = local })

	root := t.TempDir()
	const rel = ".campaign/intents/.dungeon/done/late-night.md"
	note := filepath.Join(root, filepath.FromSlash(rel))
	mustWrite(t, note, "No frontmatter, so the file time dates it.\n")
	// 23:30 on the 5th in UTC is already the 6th fourteen hours east.
	instant := time.Date(2026, 10, 5, 23, 30, 0, 0, time.UTC)
	if err := os.Chtimes(note, instant, instant); err != nil {
		t.Fatal(err)
	}

	time.Local = time.UTC
	built, err := Build(ctx, root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := itemAt(t, built, rel); got.DoneDate != "2026-10-05" || got.DateSource != DateFileTime {
		t.Fatalf("row built in UTC = %+v", got)
	}
	cacheDir := filepath.Join(t.TempDir(), "dungeon-feed")
	if err := SaveCache(ctx, cacheDir, built); err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}
	if _, ok, err := LoadCache(ctx, cacheDir); err != nil || !ok {
		t.Fatalf("same zone: ok %v, err %v; want the cache used", ok, err)
	}

	time.Local = time.FixedZone("UTC+14", 14*60*60)
	if cached, ok, err := LoadCache(ctx, cacheDir); err != nil || ok {
		t.Fatalf("fourteen hours east: cache trusted with %+v (err %v)", cached.Items, err)
	}
	rebuilt, err := Build(ctx, root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := itemAt(t, rebuilt, rel); got.DoneDate != "2026-10-06" {
		t.Fatalf("row built fourteen hours east = %+v, want the next day", got)
	}
	if err := SaveCache(ctx, cacheDir, rebuilt); err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}
	if cached, ok, err := LoadCache(ctx, cacheDir); err != nil || !ok || itemAt(t, cached, rel).DoneDate != "2026-10-06" {
		t.Fatalf("the cache written there: ok %v, err %v", ok, err)
	}

	time.Local = time.UTC
	if _, ok, err := LoadCache(ctx, cacheDir); err != nil || ok {
		t.Fatalf("back in UTC with the eastern cache: ok %v, err %v; want a miss", ok, err)
	}
}

func TestBuildHoldingDirectoryItems(t *testing.T) {
	root := t.TempDir()
	itemDir := filepath.Join(root, ".dungeon", "design-one")
	mustWrite(t, filepath.Join(itemDir, ".workitem"), "title: Design one\nref: WI-one\n")
	mustWrite(t, filepath.Join(itemDir, "README.md"), "# Design\n\nHolding summary.\n")
	mustWrite(t, filepath.Join(itemDir, "notes.md"), "Not a feed row.")
	mustWrite(t, filepath.Join(root, ".dungeon", "custom-status", "README.md"), "Custom status child")
	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	holding, err := Apply(idx, Query{Status: StatusHolding})
	if err != nil {
		t.Fatal(err)
	}
	if len(holding.Items) != 1 {
		t.Fatalf("holding: %+v", holding.Items)
	}
	// Previously cached indices classified this directory as a status. Force a
	// rebuild on upgrade even when the dungeon fingerprint has not changed.
	cache := filepath.Join(root, ".cache")
	mustWrite(t, filepath.Join(cache, "index.json"), `{"schema_version":"camp-dungeon-explore-cache/v2","index":{"items":[]}}`)
	if _, hit, err := LoadCache(context.Background(), cache); err != nil || hit {
		t.Fatalf("old holding layout cache accepted: %v", err)
	}
	if err := SaveCache(context.Background(), cache, idx); err != nil {
		t.Fatal(err)
	}
	if cached, hit, err := LoadCache(context.Background(), cache); err != nil || !hit || len(cached.Items) != len(idx.Items) {
		t.Fatalf("current cache failed roundtrip: %v", err)
	}
	item := holding.Items[0]
	if item.Path != ".dungeon/design-one" || !item.IsDir || item.Kind != KindWorkitem || item.Title != "Design one" || item.ID != "WI-one" || item.Summary != "Holding summary." {
		t.Fatalf("holding item: %+v", item)
	}
	all, err := Apply(idx, Query{Status: "all"})
	if err != nil || len(all.Items) != 2 {
		t.Fatalf("all: %+v, %v", all.Items, err)
	}
	custom, err := Apply(idx, Query{Status: "custom-status"})
	if err != nil || len(custom.Items) != 1 {
		t.Fatalf("custom status: %+v, %v", custom.Items, err)
	}
	mustWrite(t, filepath.Join(itemDir, ".workitem"), "title: Revised holding item\nref: WI-one\n")
	refreshed, changed, err := Refresh(context.Background(), root, idx)
	if err != nil || !changed {
		t.Fatalf("refresh: %v, changed=%v", err, changed)
	}
	holding, err = Apply(refreshed, Query{Status: StatusHolding})
	if err != nil || holding.Items[0].Title != "Revised holding item" {
		t.Fatalf("holding metadata stale: %+v, %v", holding, err)
	}
}

// JSON text is UTF-8. A name in another encoding would come back from the
// cache with U+FFFD where its bytes were, as a path that is not on disk, and
// the fingerprints would still match. Such an index is not cached, and a
// cache written before the name appeared is removed.
func TestCacheLeavesOutAnIndexJSONCannotCarry(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	done := filepath.Join(root, ".dungeon", "completed")
	mustWrite(t, filepath.Join(done, "plain.md"), "Plain body.\n")
	dir := filepath.Join(t.TempDir(), "dungeon-feed")

	before, err := Build(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(ctx, dir, before); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := LoadCache(ctx, dir); !ok {
		t.Fatal("a camp of UTF-8 names was not cached")
	}

	latin := "caf\xe9.md"
	mustWrite(t, filepath.Join(done, latin), "Latin body.\n")
	idx, err := Build(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	item := itemAt(t, idx, ".dungeon/completed/"+latin)
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(item.Path))); err != nil {
		t.Fatalf("the built row does not lead to its file: %v", err)
	}
	if err := SaveCache(ctx, dir, idx); err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}
	if cached, ok, _ := LoadCache(ctx, dir); ok {
		t.Fatalf("a cache is on disk for an index JSON cannot carry: %+v", cached.Items)
	}
	if _, err := os.Lstat(filepath.Join(dir, "index.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the earlier cache was left in place: %v", err)
	}

	// A status directory and a dungeon's parent are names from disk too.
	for name, bad := range map[string]Index{
		"status":       {Items: []Item{{Path: "x", Status: "stat\xfcs"}}},
		"replay":       {Items: []Item{{Path: "x", Replay: "x/r\xe9.gif"}}},
		"dungeon path": {Items: []Item{{Path: "x", DungeonPath: "caf\xe9/.dungeon"}}},
		"dungeon":      {Dungeons: []DungeonPrint{{Path: "caf\xe9/.dungeon"}}},
	} {
		fresh := filepath.Join(t.TempDir(), "dungeon-feed")
		if err := SaveCache(ctx, fresh, bad); err != nil {
			t.Fatalf("%s: SaveCache() error = %v", name, err)
		}
		if _, ok, _ := LoadCache(ctx, fresh); ok {
			t.Fatalf("%s: an index JSON cannot carry was cached", name)
		}
	}
}
