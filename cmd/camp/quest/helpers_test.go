//go:build dev

package quest

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/quest"
)

func TestAutoCommitQuest_RemoveCachedFailureReturnsError(t *testing.T) {
	root := t.TempDir()

	qctx := &questCommandContext{
		cfg:          &config.CampaignConfig{ID: "camp1234"},
		campaignRoot: root,
		service:      quest.NewService(root),
	}

	result := &quest.MutationResult{
		Quest: &quest.Quest{
			ID:   "qst_20260313_abc123",
			Name: "Lifecycle Quest",
		},
		PreStaged: []string{filepath.Join(root, ".campaign", "quests", "old-quest")},
	}

	err := autoCommitQuest(context.Background(), qctx, commit.QuestComplete, result, "Completed quest")
	if err == nil {
		t.Fatal("expected autoCommitQuest to fail when staging the old path deletion fails")
	}
	if !strings.Contains(err.Error(), "stage deletion of old quest path for auto-commit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOutputQuestListJSONUsesCampaignRelativePaths(t *testing.T) {
	root := t.TempDir()
	questPath := filepath.Join(root, ".campaign", "quests", "example", "quest.yaml")
	if err := os.MkdirAll(filepath.Dir(questPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(questPath, []byte("id: qst_example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	qctx := &questCommandContext{
		cfg:          &config.CampaignConfig{ID: "camp1234"},
		campaignRoot: root,
		service:      quest.NewService(root),
	}
	now := time.Now().UTC()
	q := &quest.Quest{
		ID:        "qst_example",
		Name:      "Example",
		Status:    quest.StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Slug:      "example",
		Path:      questPath,
	}

	stdout, err := captureQuestStdout(func() error {
		return outputQuestListJSON(qctx, []*quest.Quest{q})
	})
	if err != nil {
		t.Fatalf("outputQuestListJSON: %v", err)
	}

	var payload questListJSONPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("quest JSON invalid: %v\nraw: %s", err, stdout)
	}
	if payload.CampaignRoot != root {
		t.Fatalf("campaign_root = %q, want %q", payload.CampaignRoot, root)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items length = %d, want 1", len(payload.Items))
	}
	path := payload.Items[0].Path
	if filepath.IsAbs(path) {
		t.Fatalf("quest path is absolute: %q", path)
	}
	if _, err := os.Stat(filepath.Join(payload.CampaignRoot, path)); err != nil {
		t.Fatalf("joined quest path missing for %q: %v", path, err)
	}
	if q.Path != questPath {
		t.Fatalf("output mutated original quest path: %q", q.Path)
	}
}

func captureQuestStdout(fn func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout

	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		return "", readErr
	}
	return string(out), runErr
}
