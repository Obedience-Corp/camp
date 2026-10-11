//go:build container_fs

package explore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

func TestBuildEmptyStatusDirectories(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	for _, status := range []string{"reviewed", "completed", "all", "finished"} {
		if err := os.MkdirAll(filepath.Join(root, ".dungeon", status), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := Build(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"reviewed", "reviewed/", "REVIEWED", "completed/", "all/", "finished/"} {
		result, err := Apply(idx, Query{Status: status})
		if err != nil || len(result.Items) != 0 {
			t.Errorf("empty directory %q: %d items, %v", status, len(result.Items), err)
		}
	}
	// Removing the last row must not invalidate the directory selector, even
	// across cache publication and the next launch's freshness check.
	path := filepath.Join(root, ".dungeon", "reviewed", "item.md")
	mustWrite(t, path, "# Review\n")
	idx, err = Build(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	idx, changed, err := Refresh(ctx, root, idx)
	if err != nil || !changed {
		t.Fatalf("refresh after removal: changed=%v, %v", changed, err)
	}
	cache := t.TempDir()
	if err := SaveCache(ctx, cache, idx); err != nil {
		t.Fatal(err)
	}
	cached, ok, err := LoadCache(ctx, cache)
	if err != nil || !ok {
		t.Fatalf("cache: ok=%v, %v", ok, err)
	}
	result, err := Apply(cached, Query{Status: "reviewed"})
	if err != nil || len(result.Items) != 0 {
		t.Fatalf("cached empty directory: %+v, %v", result, err)
	}
	if _, err := Apply(cached, Query{Status: "absent"}); err == nil {
		t.Fatal("nonexistent directory accepted")
	}
}

func TestBuildMetadataYAMLCompatibility(t *testing.T) {
	for _, source := range []string{"merged", "indented"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			const preamble = "version: v1alpha9\nkind: workitem\nid: fixture-work\ntype: design\n"
			work := preamble + "title: Expected work\nref: WI-abc123\n"
			fest := "metadata:\n  id: YE0001\n  name: Expected festival\n  goal: Expected goal\n"
			if source == "merged" {
				work = preamble + "<<: {title: Expected work, ref: WI-abc123}\n"
				fest = "metadata:\n  <<: {id: YE0001, name: Expected festival, goal: Expected goal}\n"
			} else {
				work = "  version: v1alpha9\n  kind: workitem\n  id: fixture-work\n  type: design\n  title: Expected work\n  ref: WI-abc123\n"
				fest = "  metadata:\n    id: YE0001\n    name: Expected festival\n    goal: Expected goal\n"
			}
			mustWrite(t, filepath.Join(root, ".dungeon/completed/work/.workitem"), work)
			mustWrite(t, filepath.Join(root, ".dungeon/completed/festival/fest.yaml"), fest)
			reference, err := workitem.LoadMetadata(context.Background(), filepath.Join(root, ".dungeon/completed/work"))
			if err != nil || reference == nil || reference.Title != "Expected work" || reference.Ref != "WI-abc123" {
				t.Fatalf("fixture must be valid for the owning workitem loader: %+v, %v", reference, err)
			}
			idx, err := Build(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range []struct{ path, title, id string }{{".dungeon/completed/work", "Expected work", "WI-abc123"}, {".dungeon/completed/festival", "Expected festival", "YE0001"}} {
				item := itemAt(t, idx, row.path)
				if item.Title != row.title || item.ID != row.id || item.Warning != "" {
					t.Errorf("metadata lost: %+v", item)
				}
				matches, err := Apply(idx, Query{Text: row.title})
				if err != nil || len(matches.Items) != 1 {
					t.Errorf("title search %q: %+v, %v", row.title, matches, err)
				}
			}
		})
	}
}
