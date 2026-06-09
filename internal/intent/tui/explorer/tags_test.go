package explorer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestPersistTags_Intent(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, intent.CreateOptions{
		Title:     "taggable intent",
		Type:      intent.TypeIdea,
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	cmd := m.persistTags(created, []string{"personal", "follow-up"})
	if msg, ok := cmd().(tagsUpdatedMsg); !ok || msg.err != nil {
		t.Fatalf("persistTags failed: %#v", cmd())
	}

	reloaded, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(reloaded.Tags) != 2 {
		t.Fatalf("tags = %v, want 2", reloaded.Tags)
	}
}

func TestPersistTags_Note(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	note, err := svc.CreateNote(ctx, intent.CreateOptions{
		Title:     "taggable note",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	cmd := m.persistTags(note, []string{"reference"})
	if msg, ok := cmd().(tagsUpdatedMsg); !ok || msg.err != nil {
		t.Fatalf("persistTags failed: %#v", cmd())
	}

	reloaded, err := svc.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if len(reloaded.Tags) != 1 || reloaded.Tags[0] != "reference" {
		t.Fatalf("note tags = %v, want [reference]", reloaded.Tags)
	}
}
