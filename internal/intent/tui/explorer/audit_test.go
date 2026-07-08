package explorer

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/git/commit"
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

func TestAutoCommitIntentReturnsBeforeCommitFinishes(t *testing.T) {
	oldRun := runAutoCommitIntent
	defer func() { runAutoCommitIntent = oldRun }()

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	runAutoCommitIntent = func(ctx context.Context, opts commit.IntentOptions) {
		close(started)
		<-release
		close(done)
	}

	campaignRoot := filepath.Join(string(filepath.Separator), "tmp", "campaign")
	intentsDir := filepath.Join(campaignRoot, ".campaign", "intents")
	m := NewModel(context.Background(), nil, nil, intentsDir, campaignRoot, "test-id", "", nil)

	returned := make(chan struct{})
	go func() {
		m.autoCommitIntent(commit.IntentMove, "Fast action", "Moved", filepath.Join(intentsDir, "foo.md"))
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("autoCommitIntent blocked waiting for commit completion")
	}

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("background auto-commit did not start")
	}

	select {
	case <-done:
		t.Fatal("test commit finished before release; blocking stub was not used")
	default:
	}

	close(release)
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("background auto-commit did not finish after release")
	}
}
