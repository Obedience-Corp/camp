package workitem

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestTrackedFileIndexFilesUnder(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "camp")
	item := filepath.Join(root, "workflow", "design", "alpha")
	sibling := filepath.Join(root, "workflow", "design", "alphabet")
	index := &trackedFileIndex{
		root: root,
		files: []string{
			filepath.Join(item, "README.md"),
			filepath.Join(item, "notes", "detail.md"),
			filepath.Join(sibling, "README.md"),
		},
	}

	if !index.contains(item) || index.contains(filepath.Join(root, "..", "other")) {
		t.Fatal("index must serve only directories in its camp root")
	}
	want := index.files[:2]
	if got := index.filesUnder(item); !reflect.DeepEqual(got, want) {
		t.Fatalf("filesUnder(%q) = %v, want %v", item, got, want)
	}
	if got := index.filesUnder(filepath.Join(root, "workflow", "explore", "missing")); len(got) != 0 {
		t.Fatalf("missing directory returned files: %v", got)
	}
}
