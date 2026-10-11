//go:build container_fs

package explore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

func TestExploreCacheSaveRunsOffTheUpdateLoop(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "dungeon-feed")
	m := replayModel(intexplore.ProtocolOff)
	m.ctx = ctx
	m.cacheDir = dir
	idx := refreshIndex("Saved")
	m, cmd := press(t, m, exploreLoaded{gen: m.indexGen, index: idx, changed: true})
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("Update wrote the cache itself (%v)", err)
	}
	if cmd == nil {
		t.Fatal("a changed index scheduled no cache save")
	}
	// With images off nothing else is scheduled, so the command is the save.
	if msg := cmd(); msg != nil {
		t.Fatalf("save command sent %T", msg)
	}
	cached, ok, err := intexplore.LoadCache(ctx, dir)
	if err != nil || !ok || len(cached.Items) != 1 || cached.Items[0].Title != "Saved" {
		t.Fatalf("saved cache: ok %v, err %v, items %+v", ok, err, cached.Items)
	}

	_, cmd = press(t, m, exploreLoaded{gen: m.indexGen, index: idx})
	if cmd != nil {
		t.Fatal("an unchanged index scheduled a cache save")
	}
}
