//go:build dev

package quest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
