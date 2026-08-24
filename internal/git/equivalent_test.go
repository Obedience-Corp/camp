package git

import (
	"context"
	"testing"
)

func TestBranchesEquivalentToRef_EmptyBaseRef(t *testing.T) {
	_, err := BranchesEquivalentToRef(context.Background(), "/nope", "", []string{"feat"})
	if err == nil {
		t.Fatal("expected error for empty base ref")
	}
}

func TestBranchesEquivalentToRef_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := BranchesEquivalentToRef(ctx, "/nope", "main", []string{"feat"})
	if err == nil {
		t.Fatal("expected cancelled context error")
	}
}

func TestBranchesEquivalentToRef_EmptyBranches(t *testing.T) {
	got, err := BranchesEquivalentToRef(context.Background(), "/nope", "main", nil)
	if err != nil {
		t.Fatalf("empty branches: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty branches: got %v, want empty set", got)
	}
}
