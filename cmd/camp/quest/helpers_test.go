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
	if payload.SchemaVersion != QuestListJSONVersion {
		t.Fatalf("schema_version = %q, want %q", payload.SchemaVersion, QuestListJSONVersion)
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

func TestOutputQuestTableRendersColumns(t *testing.T) {
	root := t.TempDir()
	qctx := &questCommandContext{
		cfg:          &config.CampaignConfig{ID: "camp1234"},
		campaignRoot: root,
		service:      quest.NewService(root),
	}
	now := time.Now().UTC()
	quests := []*quest.Quest{
		{
			ID:        "qst_abc",
			Name:      "Alpha Quest",
			Status:    quest.StatusOpen,
			CreatedAt: now,
			UpdatedAt: now,
			Slug:      "alpha-quest",
			Path:      filepath.Join(root, ".campaign", "quests", "alpha-quest"),
		},
		{
			ID:        "qst_def",
			Name:      "Beta Quest",
			Status:    quest.StatusPaused,
			CreatedAt: now,
			UpdatedAt: now,
			Slug:      "beta-quest",
			Path:      filepath.Join(root, ".campaign", "quests", "beta-quest"),
		},
	}

	out, err := captureQuestStdout(func() error {
		return outputQuestTable(qctx, quests)
	})
	if err != nil {
		t.Fatalf("outputQuestTable: %v", err)
	}

	for _, col := range []string{"NAME", "STATUS", "ID", "UPDATED", "PATH"} {
		if !strings.Contains(out, col) {
			t.Errorf("outputQuestTable output missing column header %q", col)
		}
	}
	if !strings.Contains(out, "Alpha Quest") {
		t.Error("outputQuestTable output missing quest name Alpha Quest")
	}
	if !strings.Contains(out, "qst_abc") {
		t.Error("outputQuestTable output missing quest ID qst_abc")
	}
	if !strings.Contains(out, "2 quest(s)") {
		t.Errorf("outputQuestTable output missing count line, got:\n%s", out)
	}
}

func TestOutputQuestTable_Empty(t *testing.T) {
	root := t.TempDir()
	qctx := &questCommandContext{
		cfg:          &config.CampaignConfig{ID: "camp1234"},
		campaignRoot: root,
		service:      quest.NewService(root),
	}

	out, err := captureQuestStdout(func() error {
		return outputQuestTable(qctx, nil)
	})
	if err != nil {
		t.Fatalf("outputQuestTable: %v", err)
	}
	if !strings.Contains(out, "No quests found") {
		t.Errorf("expected empty message, got: %s", out)
	}
}

func TestOutputQuestTable_JSONUnchanged(t *testing.T) {
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
		ID:        "qst_json_check",
		Name:      "JSON Check",
		Status:    quest.StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Slug:      "json-check",
		Path:      questPath,
	}

	out, err := captureQuestStdout(func() error {
		return outputQuestListJSON(qctx, []*quest.Quest{q})
	})
	if err != nil {
		t.Fatalf("outputQuestListJSON: %v", err)
	}

	var payload questListJSONPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("JSON output still valid after lipgloss refactor: %v\nraw: %s", err, out)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "qst_json_check" {
		t.Fatalf("unexpected JSON payload: %+v", payload)
	}
}

func TestOutputQuestShowJSONUsesSchemaVersionAndCampaignRelativePath(t *testing.T) {
	root := t.TempDir()
	questPath := filepath.Join(root, ".campaign", "quests", "example", "quest.yaml")
	if err := os.MkdirAll(filepath.Dir(questPath), 0o755); err != nil {
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

	out, err := captureQuestStdout(func() error {
		return outputQuestShowJSON(qctx, q)
	})
	if err != nil {
		t.Fatalf("outputQuestShowJSON: %v", err)
	}

	var payload questShowJSONPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("quest show JSON invalid: %v\nraw: %s", err, out)
	}
	if payload.SchemaVersion != QuestShowJSONVersion {
		t.Fatalf("schema_version = %q, want %q", payload.SchemaVersion, QuestShowJSONVersion)
	}
	if payload.CampaignRoot != root {
		t.Fatalf("campaign_root = %q, want %q", payload.CampaignRoot, root)
	}
	if payload.Quest == nil {
		t.Fatal("quest is nil")
	}
	if filepath.IsAbs(payload.Quest.Path) {
		t.Fatalf("quest path is absolute: %q", payload.Quest.Path)
	}
	if q.Path != questPath {
		t.Fatalf("output mutated original quest path: %q", q.Path)
	}
}

func TestAutoCommitQuestNilResultIsNoOp(t *testing.T) {
	root := t.TempDir()
	qctx := &questCommandContext{
		cfg:          &config.CampaignConfig{ID: "camp1234"},
		campaignRoot: root,
		service:      quest.NewService(root),
	}
	if err := autoCommitQuest(context.Background(), qctx, commit.QuestComplete, nil, ""); err != nil {
		t.Fatalf("expected nil result to be a no-op, got: %v", err)
	}
}
