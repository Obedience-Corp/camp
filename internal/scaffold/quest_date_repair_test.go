package scaffold

import (
	"context"
	"testing"
	"time"
)

func TestComputeQuestSentinelDateChange_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := &RepairPlan{}
	if err := computeQuestSentinelDateChange(ctx, "/nonexistent", plan); err == nil {
		t.Fatal("expected a context error, got nil")
	}
	if plan.QuestDateBackfill != nil {
		t.Error("a cancelled context must not stage a backfill")
	}
}

func TestApplyQuestDateBackfill_NilIsNoop(t *testing.T) {
	if err := applyQuestDateBackfill(context.Background(), nil); err != nil {
		t.Fatalf("nil backfill must be a no-op, got %v", err)
	}
}

func TestApplyQuestDateBackfill_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	bf := &QuestDateBackfill{Path: "/nonexistent/quest.yaml", Replacement: time.Now().UTC()}
	if err := applyQuestDateBackfill(ctx, bf); err == nil {
		t.Fatal("expected a context error, got nil")
	}
}
