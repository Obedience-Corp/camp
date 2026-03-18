package explorer

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAutoCommitFiles_IncludesAuditLog(t *testing.T) {
	campaignRoot := filepath.Join(string(filepath.Separator), "tmp", "campaign")
	intentsDir := filepath.Join(campaignRoot, ".campaign", "intents")

	m := NewModel(context.Background(), nil, nil, intentsDir, campaignRoot, "test-id", "", nil)

	got := m.autoCommitFiles(filepath.Join(intentsDir, "foo.md"))
	want := []string{
		filepath.Join(".campaign", "intents", "foo.md"),
		filepath.Join(".campaign", "intents", ".intents.jsonl"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("autoCommitFiles() = %#v, want %#v", got, want)
	}
}

func TestAutoCommitFiles_SkipsAuditLogWithoutIntentsDir(t *testing.T) {
	campaignRoot := filepath.Join(string(filepath.Separator), "tmp", "campaign")

	m := NewModel(context.Background(), nil, nil, "", campaignRoot, "test-id", "", nil)

	got := m.autoCommitFiles(filepath.Join(campaignRoot, "workflow", "intents", "foo.md"))
	want := []string{filepath.Join("workflow", "intents", "foo.md")}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("autoCommitFiles() = %#v, want %#v", got, want)
	}
}
