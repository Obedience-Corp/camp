package dungeon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeMarker(t *testing.T, dir, wfType string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: v1alpha8\nkind: workitem\nid: " + wfType + "-thing-1\ntype: " + wfType + "\ntitle: Thing\n"
	if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itemsByName(items []DungeonItem) map[string]DungeonItem {
	out := make(map[string]DungeonItem, len(items))
	for _, it := range items {
		out[it.Name] = it
	}
	return out
}

// Triage over a parent shows a directory workitem's real type. A plain directory
// and a loose file must be untouched, since the field is additive.
func TestListParentItems_PopulatesWorkitemType(t *testing.T) {
	parent := t.TempDir()
	writeMarker(t, filepath.Join(parent, "resident"), "design")
	if err := os.MkdirAll(filepath.Join(parent, "plaindir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "loose.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewService(parent, filepath.Join(parent, "dungeon"))
	items, err := svc.ListParentItems(context.Background(), parent)
	if err != nil {
		t.Fatalf("ListParentItems: %v", err)
	}
	got := itemsByName(items)

	res, ok := got["resident"]
	if !ok {
		t.Fatalf("resident not listed: %+v", items)
	}
	if res.WorkitemType != "design" {
		t.Errorf("WorkitemType = %q, want design", res.WorkitemType)
	}
	if res.Type != ItemTypeDirectory {
		t.Errorf("Type = %q, want directory: the generic label must not be replaced", res.Type)
	}

	if plain, ok := got["plaindir"]; ok && plain.WorkitemType != "" {
		t.Errorf("plain directory got WorkitemType %q, want empty", plain.WorkitemType)
	}
	if loose, ok := got["loose.md"]; ok && loose.WorkitemType != "" {
		t.Errorf("file got WorkitemType %q, want empty", loose.WorkitemType)
	}
}

// An unreadable marker must not fail the listing.
func TestListParentItems_UnreadableMarkerIsNotFatal(t *testing.T) {
	parent := t.TempDir()
	broken := filepath.Join(parent, "broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, ".workitem"), []byte("kind: workitem\n\tbad: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewService(parent, filepath.Join(parent, "dungeon"))
	items, err := svc.ListParentItems(context.Background(), parent)
	if err != nil {
		t.Fatalf("an unreadable marker must not fail the listing: %v", err)
	}
	if item, ok := itemsByName(items)["broken"]; !ok {
		t.Error("directory with a broken marker should still be listed")
	} else if item.WorkitemType != "" {
		t.Errorf("WorkitemType = %q, want empty for an unreadable marker", item.WorkitemType)
	}
}

func TestBuildInfoString_WorkitemType(t *testing.T) {
	plain := DungeonItem{Name: "d", Type: ItemTypeDirectory}
	if got := buildInfoString(plain, nil); got[:16] != "Type: directory " {
		t.Errorf("plain directory line changed: %q", got)
	}

	resident := DungeonItem{Name: "d", Type: ItemTypeDirectory, WorkitemType: "design"}
	got := buildInfoString(resident, nil)
	if want := "Type: directory (design) | Modified: "; got[:len(want)] != want {
		t.Errorf("got %q, want prefix %q", got, want)
	}
}
