package explore

import (
	"path/filepath"
	"testing"
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
