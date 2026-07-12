package explorer

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/git/commit"
)

func captureSlogWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func seedExplorerAmbientCampaign(t *testing.T, withWorkitem bool) (root, wiDir string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.campaign): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("id: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(campaign.yaml): %v", err)
	}

	wiDir = filepath.Join(root, "workflow", "design", "example")
	if err := os.MkdirAll(wiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(wiDir): %v", err)
	}
	if !withWorkitem {
		return root, wiDir
	}
	marker := "version: v1alpha6\nkind: workitem\nid: design-example-2026-05-24\ntype: design\ntitle: Example\nref: WI-abc123\n"
	if err := os.WriteFile(filepath.Join(wiDir, ".workitem"), []byte(marker), 0o644); err != nil {
		t.Fatalf("WriteFile(.workitem): %v", err)
	}
	return root, wiDir
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd(): %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

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
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	campaignRoot := filepath.Join(string(filepath.Separator), "tmp", "campaign")
	intentsDir := filepath.Join(campaignRoot, ".campaign", "intents")
	m := NewModel(context.Background(), nil, nil, intentsDir, campaignRoot, "test-id", "", nil)
	m.autoCommit.run = func(ctx context.Context, opts commit.IntentOptions) {
		close(started)
		<-release
		close(done)
	}

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

func TestAutoCommitIntentSerializesBackgroundCommits(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	done := make(chan struct{})
	calls := 0

	campaignRoot := filepath.Join(string(filepath.Separator), "tmp", "campaign")
	intentsDir := filepath.Join(campaignRoot, ".campaign", "intents")
	m := NewModel(context.Background(), nil, nil, intentsDir, campaignRoot, "test-id", "", nil)
	m.autoCommit.run = func(ctx context.Context, opts commit.IntentOptions) {
		calls++
		switch calls {
		case 1:
			close(firstStarted)
			<-releaseFirst
		case 2:
			close(secondStarted)
			close(done)
		default:
			t.Fatalf("unexpected auto-commit call %d", calls)
		}
	}

	m.autoCommitIntent(commit.IntentMove, "First action", "Moved", filepath.Join(intentsDir, "first.md"))
	select {
	case <-firstStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("first background auto-commit did not start")
	}

	m.autoCommitIntent(commit.IntentMove, "Second action", "Moved", filepath.Join(intentsDir, "second.md"))
	select {
	case <-secondStarted:
		t.Fatal("second background auto-commit started while first commit was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseFirst)
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second background auto-commit did not start after first commit released")
	}
}

func TestAutoCommitterDrainsInFlightCommit(t *testing.T) {
	release := make(chan struct{})
	committed := make(chan struct{})

	campaignRoot := filepath.Join(string(filepath.Separator), "tmp", "campaign")
	intentsDir := filepath.Join(campaignRoot, ".campaign", "intents")
	m := NewModel(context.Background(), nil, nil, intentsDir, campaignRoot, "test-id", "", nil)
	m.autoCommit.run = func(ctx context.Context, opts commit.IntentOptions) {
		<-release
		close(committed)
	}

	m.autoCommitIntent(commit.IntentMove, "Pending action", "Moved", filepath.Join(intentsDir, "pending.md"))

	if m.autoCommit.wait(50 * time.Millisecond) {
		t.Fatal("wait reported drained while a commit was still in flight")
	}

	close(release)

	if !m.autoCommit.wait(time.Second) {
		t.Fatal("wait did not drain after the commit finished")
	}

	select {
	case <-committed:
	default:
		t.Fatal("auto-commit did not run to completion before drain returned")
	}
}

func TestDrainAutoCommitsSilentWhenNothingPending(t *testing.T) {
	m := NewModel(context.Background(), nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	var buf bytes.Buffer
	m.DrainAutoCommits(&buf)
	if buf.Len() != 0 {
		t.Fatalf("expected no output when no commits pending, got %q", buf.String())
	}
}

func TestAutoCommitIntent_AmbientContext(t *testing.T) {
	tests := []struct {
		name            string
		withWorkitem    bool
		wantWorkitemRef string
	}{
		{
			name:            "context present inherits workitem ref",
			withWorkitem:    true,
			wantWorkitemRef: "WI-abc123",
		},
		{
			name:            "context absent falls back to bare tag",
			withWorkitem:    false,
			wantWorkitemRef: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, wiDir := seedExplorerAmbientCampaign(t, tt.withWorkitem)
			chdirForTest(t, wiDir)

			intentsDir := filepath.Join(root, ".campaign", "intents")
			m := NewModel(context.Background(), nil, nil, intentsDir, root, "campaign-id", "", nil)

			captured := make(chan commit.IntentOptions, 1)
			m.autoCommit.run = func(ctx context.Context, opts commit.IntentOptions) {
				captured <- opts
			}

			m.autoCommitIntent(commit.IntentMove, "Test Intent", "desc", filepath.Join(intentsDir, "foo.md"))

			select {
			case opts := <-captured:
				if opts.WorkitemRef != tt.wantWorkitemRef {
					t.Errorf("WorkitemRef = %q, want %q", opts.WorkitemRef, tt.wantWorkitemRef)
				}
				if opts.CampaignRoot != root {
					t.Errorf("CampaignRoot = %q, want %q", opts.CampaignRoot, root)
				}
				if opts.CampaignID != "campaign-id" {
					t.Errorf("CampaignID = %q, want %q", opts.CampaignID, "campaign-id")
				}
			case <-time.After(time.Second):
				t.Fatal("background auto-commit did not run")
			}
		})
	}
}

func TestAutoCommitIntent_SkipsMissingCampaignContext(t *testing.T) {
	buf := captureSlogWarnings(t)

	m := NewModel(context.Background(), nil, nil, "", "", "", "", nil)
	ran := false
	m.autoCommit.run = func(ctx context.Context, opts commit.IntentOptions) {
		ran = true
	}

	m.autoCommitIntent(commit.IntentMove, "Test Intent", "desc", "some/file.md")

	if ran {
		t.Fatal("expected auto-commit to be skipped when campaign context is missing, but run was invoked")
	}
	logged := buf.String()
	if !strings.Contains(logged, "intent auto-commit skipped") {
		t.Fatalf("expected skip warning logged, got: %s", logged)
	}
	if !strings.Contains(logged, "missing campaign context") {
		t.Fatalf("expected missing-campaign-context reason logged, got: %s", logged)
	}
}

func TestRunAutoCommitIntent_LogsSkipWarningForEmptySelectiveScope(t *testing.T) {
	buf := captureSlogWarnings(t)

	runAutoCommitIntent(context.Background(), commit.IntentOptions{
		Options: commit.Options{
			CampaignRoot:  "/some/campaign",
			CampaignID:    "campaign-id",
			SelectiveOnly: true,
		},
		Action:      commit.IntentMove,
		IntentTitle: "Test Intent",
	})

	logged := buf.String()
	if !strings.Contains(logged, "intent auto-commit skipped") {
		t.Fatalf("expected skip warning logged, got: %s", logged)
	}
	if !strings.Contains(logged, "no files resolved to stage") {
		t.Fatalf("expected skip reason logged, got: %s", logged)
	}
}
