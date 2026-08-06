package workitem

import (
	"strings"
	"testing"
)

func TestCarriesCommitRef(t *testing.T) {
	tests := []struct {
		name string
		wi   *WorkItem
		want bool
	}{
		{"nil", nil, false},
		{"file kind without stable id", &WorkItem{ItemKind: ItemKindFile}, false},
		{
			"intent identifies itself but has no marker to hold a ref",
			&WorkItem{WorkflowType: WorkflowTypeIntent, ItemKind: ItemKindFile, SourceID: "idea-20260101-000000"},
			false,
		},
		{
			"festival identifies itself but has no marker to hold a ref",
			&WorkItem{WorkflowType: WorkflowTypeFestival, ItemKind: ItemKindDirectory, SourceID: "SC0001"},
			false,
		},
		{
			"directory without a stable id has nothing to hash",
			&WorkItem{WorkflowType: WorkflowTypeDesign, ItemKind: ItemKindDirectory},
			false,
		},
		{
			"adopted directory workitem carries a ref",
			&WorkItem{WorkflowType: WorkflowTypeDesign, ItemKind: ItemKindDirectory, StableID: "design-x-2026-07-17"},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CarriesCommitRef(tt.wi); got != tt.want {
				t.Fatalf("CarriesCommitRef() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWorktreeLinkCommitNote guards the honesty of the post-link message: it may
// only promise a WI- segment for a workitem that can actually mint one.
func TestWorktreeLinkCommitNote(t *testing.T) {
	intent := &WorkItem{WorkflowType: WorkflowTypeIntent, ItemKind: ItemKindFile, SourceID: "idea-20260101-000000"}
	if note := WorktreeLinkCommitNote(intent); strings.Contains(note, "will include WI-*") {
		t.Fatalf("intent note promises a WI- segment it cannot mint: %q", note)
	}

	adopted := &WorkItem{WorkflowType: WorkflowTypeDesign, ItemKind: ItemKindDirectory, StableID: "design-x-2026-07-17"}
	if note := WorktreeLinkCommitNote(adopted); !strings.Contains(note, "will include WI-*") {
		t.Fatalf("adopted workitem note should promise a WI- segment, got %q", note)
	}
}
