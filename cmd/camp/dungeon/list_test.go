package dungeon

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
)

func TestBuildDungeonListContextLine(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		parentRel  string
		dungeonRel string
		want       string
	}{
		{
			name:       "dungeon mode",
			source:     "dungeon",
			parentRel:  "workflow/design",
			dungeonRel: "workflow/design/dungeon",
			want:       "Context: dungeon=workflow/design/dungeon",
		},
		{
			name:       "triage mode",
			source:     "triage",
			parentRel:  "workflow/design",
			dungeonRel: "workflow/design/dungeon",
			want:       "Context: parent=workflow/design -> dungeon=workflow/design/dungeon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDungeonListContextLine(tt.source, tt.parentRel, tt.dungeonRel)
			if got != tt.want {
				t.Fatalf("buildDungeonListContextLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildDungeonEmptyMessage(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		dungeonRel string
		want       string
	}{
		{
			name:       "dungeon mode",
			source:     "dungeon",
			dungeonRel: "workflow/design/dungeon",
			want:       "Dungeon is empty (context: workflow/design/dungeon).",
		},
		{
			name:       "triage mode",
			source:     "triage",
			dungeonRel: "workflow/design/dungeon",
			want:       "No parent items eligible for triage (context: workflow/design/dungeon).",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDungeonEmptyMessage(tt.source, tt.dungeonRel)
			if got != tt.want {
				t.Fatalf("buildDungeonEmptyMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDungeonListJSONAliasRegistered(t *testing.T) {
	if dungeonListCmd.Flags().Lookup("json") == nil {
		t.Fatal("camp dungeon list missing --json alias")
	}
}

// outputDungeonJSON remaps into a local struct, so a field added to DungeonItem
// does not reach --json on its own. This locks the mapping.
func TestOutputDungeonJSON_IncludesWorkitemType(t *testing.T) {
	items := []intdungeon.DungeonItem{
		{Name: "resident", Path: "/c/workflow/design/resident", Type: intdungeon.ItemTypeDirectory, WorkitemType: "design"},
		{Name: "plaindir", Path: "/c/workflow/design/plaindir", Type: intdungeon.ItemTypeDirectory},
		{Name: "loose.md", Path: "/c/workflow/design/loose.md", Type: intdungeon.ItemTypeFile},
	}

	out := captureDungeonStdout(t, func() {
		if err := outputDungeonJSON(items); err != nil {
			t.Fatalf("outputDungeonJSON: %v", err)
		}
	})

	var decoded []map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(decoded) != 3 {
		t.Fatalf("want 3 items, got %d", len(decoded))
	}
	if got := decoded[0]["workitem_type"]; got != "design" {
		t.Errorf("resident workitem_type = %v, want design", got)
	}
	// omitempty: absent, not empty-string, for non-workitems.
	if _, ok := decoded[1]["workitem_type"]; ok {
		t.Error("plain directory must not carry workitem_type")
	}
	if _, ok := decoded[2]["workitem_type"]; ok {
		t.Error("file must not carry workitem_type")
	}
}

func captureDungeonStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
