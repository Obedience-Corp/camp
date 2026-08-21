package moveref

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestMergeRelPaths(t *testing.T) {
	t.Parallel()

	got := mergeRelPaths(
		[]string{"docs/a.md", "docs/b.md"},
		[]string{"docs/b.md", ".campaign/quests/launch/quest.yaml"},
	)
	want := []string{".campaign/quests/launch/quest.yaml", "docs/a.md", "docs/b.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeRelPaths = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(mergeRelPaths(nil, want), want) {
		t.Fatalf("nil left should return right")
	}
}

func TestRewriteMoves_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RewriteMoves(ctx, "/nonexistent", nil)
	if err == nil {
		t.Fatal("expected cancelled context error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
