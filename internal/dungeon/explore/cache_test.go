package explore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCacheDirHonorsXDGConfigHome(t *testing.T) {
	xdg := filepath.Join(string(filepath.Separator), "redirected", "config")
	home := filepath.Join(string(filepath.Separator), "home", "user")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", home)
	dir, err := CacheDir("fixture", "/camp")
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(xdg, "obey", "campaign", "caches")
	if rel, err := filepath.Rel(base, dir); err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("cache dir = %s, want it under %s", dir, base)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err = CacheDir("fixture", "/camp")
	if err != nil {
		t.Fatal(err)
	}
	base = filepath.Join(home, ".obey", "campaign", "caches")
	if rel, err := filepath.Rel(base, dir); err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("cache dir = %s, want it under %s", dir, base)
	}
}

func TestCacheDirSeparatesCheckoutsOfOneCamp(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/redirected/config")
	dirFor := func(id, root string) string {
		t.Helper()
		dir, err := CacheDir(id, root)
		if err != nil {
			t.Fatal(err)
		}
		return dir
	}
	main := dirFor("camp-id", "/src/camp")
	if worktree := dirFor("camp-id", "/src/camp-worktree"); worktree == main {
		t.Fatalf("two checkouts of one camp share the cache %s", main)
	}
	if other := dirFor("other-id", "/src/camp"); other == main {
		t.Fatalf("two camp IDs at one root share the cache %s", main)
	}
	if again := dirFor("camp-id", "/src/camp/"); again != main {
		t.Fatalf("one checkout resolved to %s and %s", main, again)
	}
}

// The key is the zone, not the offset on the day it is read. It is the same
// in January and in July of one zone, and it tells apart two zones that agree
// for only part of the year.
func TestZoneKeyNamesTheZoneAndNotTheDay(t *testing.T) {
	local := time.Local
	t.Cleanup(func() { time.Local = local })

	time.Local = time.UTC
	utc := zoneKey()
	time.Local = time.FixedZone("UTC+14", 14*60*60)
	if east := zoneKey(); east == utc || east != "50400/50400" {
		t.Fatalf("zoneKey fourteen hours east = %q, in UTC = %q", east, utc)
	}

	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("no time zone database: %v", err)
	}
	phoenix, err := time.LoadLocation("America/Phoenix")
	if err != nil {
		t.Skipf("no time zone database: %v", err)
	}
	// Both are seven hours behind UTC all winter. Denver moves in summer.
	time.Local = denver
	if got := zoneKey(); got != "-25200/-21600" {
		t.Fatalf("zoneKey in Denver = %q", got)
	}
	time.Local = phoenix
	if got := zoneKey(); got != "-25200/-25200" {
		t.Fatalf("zoneKey in Phoenix = %q", got)
	}
}

func TestCachePreservesEmptyStatusNames(t *testing.T) {
	idx := Index{Dungeons: []DungeonPrint{{Path: ".dungeon", Statuses: []string{"reviewed"}}}}
	if !survivesJSON(idx) {
		t.Fatal("ordinary status name cannot be cached")
	}
	idx.Dungeons[0].Statuses = []string{"reviewed\xff"}
	if survivesJSON(idx) {
		t.Fatal("non-UTF8 empty status would be silently renamed by the cache")
	}
}
