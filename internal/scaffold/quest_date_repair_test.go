package scaffold

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/quest"
)

func writeDefaultQuest(t *testing.T, root string, created, updated time.Time) string {
	t.Helper()
	path := quest.DefaultQuestPath(root)
	q := &quest.Quest{
		ID:        quest.DefaultQuestID,
		Name:      quest.DefaultQuestName,
		Status:    quest.StatusOpen,
		CreatedAt: created,
		UpdatedAt: updated,
	}
	if err := quest.Save(context.Background(), path, q); err != nil {
		t.Fatalf("save default quest: %v", err)
	}
	return path
}

func TestComputeQuestSentinelDateChange(t *testing.T) {
	realDate := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	t.Run("cancelled context returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		plan := &RepairPlan{}
		if err := computeQuestSentinelDateChange(ctx, t.TempDir(), plan); err == nil {
			t.Fatal("expected a context error, got nil")
		}
	})

	t.Run("missing default quest is a no-op", func(t *testing.T) {
		plan := &RepairPlan{}
		if err := computeQuestSentinelDateChange(context.Background(), t.TempDir(), plan); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.QuestDateBackfill != nil {
			t.Error("expected no backfill staged for a missing quest")
		}
		if len(plan.Changes) != 0 {
			t.Errorf("expected no changes, got %d", len(plan.Changes))
		}
	})

	t.Run("real dates are a no-op", func(t *testing.T) {
		root := t.TempDir()
		writeDefaultQuest(t, root, realDate, realDate)
		plan := &RepairPlan{}
		if err := computeQuestSentinelDateChange(context.Background(), root, plan); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.QuestDateBackfill != nil {
			t.Error("expected no backfill staged for real dates")
		}
		if len(plan.Changes) != 0 {
			t.Errorf("expected no changes, got %d", len(plan.Changes))
		}
	})

	t.Run("sentinel dates stage a backfill change", func(t *testing.T) {
		root := t.TempDir()
		path := writeDefaultQuest(t, root, questDateSentinel, questDateSentinel)
		plan := &RepairPlan{}
		if err := computeQuestSentinelDateChange(context.Background(), root, plan); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.QuestDateBackfill == nil {
			t.Fatal("expected a backfill to be staged")
		}
		if plan.QuestDateBackfill.Path != path {
			t.Errorf("backfill path = %q, want %q", plan.QuestDateBackfill.Path, path)
		}
		if plan.QuestDateBackfill.Replacement.Equal(questDateSentinel) {
			t.Error("replacement must not be the sentinel value")
		}
		if len(plan.Changes) != 1 || plan.Changes[0].Type != RepairModify {
			t.Errorf("expected one RepairModify change, got %+v", plan.Changes)
		}
	})
}

func TestApplyQuestDateBackfill(t *testing.T) {
	realDate := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	replacement := time.Date(2025, 5, 5, 9, 0, 0, 0, time.UTC)

	t.Run("nil backfill is a no-op", func(t *testing.T) {
		if err := applyQuestDateBackfill(context.Background(), nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("cancelled context returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		bf := &QuestDateBackfill{Path: filepath.Join(t.TempDir(), "quest.yaml"), Replacement: replacement}
		if err := applyQuestDateBackfill(ctx, bf); err == nil {
			t.Fatal("expected a context error, got nil")
		}
	})

	t.Run("rewrites sentinel dates to the replacement", func(t *testing.T) {
		root := t.TempDir()
		path := writeDefaultQuest(t, root, questDateSentinel, questDateSentinel)
		if err := applyQuestDateBackfill(context.Background(), &QuestDateBackfill{Path: path, Replacement: replacement}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := quest.Load(context.Background(), path)
		if err != nil {
			t.Fatalf("load quest: %v", err)
		}
		if !got.CreatedAt.Equal(replacement) || !got.UpdatedAt.Equal(replacement) {
			t.Errorf("dates = %v / %v, want %v", got.CreatedAt, got.UpdatedAt, replacement)
		}
	})

	t.Run("leaves real dates untouched", func(t *testing.T) {
		root := t.TempDir()
		path := writeDefaultQuest(t, root, realDate, realDate)
		if err := applyQuestDateBackfill(context.Background(), &QuestDateBackfill{Path: path, Replacement: replacement}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := quest.Load(context.Background(), path)
		if err != nil {
			t.Fatalf("load quest: %v", err)
		}
		if !got.CreatedAt.Equal(realDate) || !got.UpdatedAt.Equal(realDate) {
			t.Errorf("dates = %v / %v, want unchanged %v", got.CreatedAt, got.UpdatedAt, realDate)
		}
	})
}
