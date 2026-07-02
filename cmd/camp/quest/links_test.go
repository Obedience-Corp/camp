//go:build dev

package quest

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/quest"
)

func TestOutputLinksJSONUsesCampaignRelativePathsAndEnvelope(t *testing.T) {
	root, err := pathutil.ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	qctx := &questCommandContext{
		cfg:          &config.CampaignConfig{ID: "camp1234"},
		campaignRoot: root,
		service:      quest.NewService(root),
	}
	links := []quest.Link{
		{
			Path:    "workflow/design/some-design.md",
			Type:    "design",
			AddedAt: time.Now().UTC(),
		},
		{
			Path:    filepath.Join(root, "projects", "camp"),
			Type:    "project",
			AddedAt: time.Now().UTC(),
		},
	}

	stdout, err := captureQuestStdout(func() error {
		return outputLinksJSON(qctx, links)
	})
	if err != nil {
		t.Fatalf("outputLinksJSON: %v", err)
	}

	var payload questLinksJSONPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("links JSON invalid: %v\nraw: %s", err, stdout)
	}
	if payload.SchemaVersion != QuestLinksJSONVersion {
		t.Fatalf("schema_version = %q, want %q", payload.SchemaVersion, QuestLinksJSONVersion)
	}
	if payload.CampaignRoot != root {
		t.Fatalf("campaign_root = %q, want %q", payload.CampaignRoot, root)
	}
	if len(payload.Links) != 2 {
		t.Fatalf("links length = %d, want 2", len(payload.Links))
	}
	for _, link := range payload.Links {
		if filepath.IsAbs(link.Path) {
			t.Fatalf("link path is absolute: %q", link.Path)
		}
	}
	if payload.Links[0].Path != "workflow/design/some-design.md" {
		t.Fatalf("already-relative link path changed: %q", payload.Links[0].Path)
	}
	if payload.Links[1].Path != filepath.Join("projects", "camp") {
		t.Fatalf("absolute link path not relativized: %q", payload.Links[1].Path)
	}
	if links[1].Path != filepath.Join(root, "projects", "camp") {
		t.Fatalf("outputLinksJSON mutated the caller's link slice: %q", links[1].Path)
	}
}

func TestOutputLinksJSONEmitsEmptyArrayNotNull(t *testing.T) {
	root := t.TempDir()
	qctx := &questCommandContext{
		cfg:          &config.CampaignConfig{ID: "camp1234"},
		campaignRoot: root,
		service:      quest.NewService(root),
	}

	stdout, err := captureQuestStdout(func() error {
		return outputLinksJSON(qctx, nil)
	})
	if err != nil {
		t.Fatalf("outputLinksJSON: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("links JSON invalid: %v\nraw: %s", err, stdout)
	}
	if got := string(raw["links"]); got != "[]" {
		t.Fatalf("links = %s, want empty array []", got)
	}
}
