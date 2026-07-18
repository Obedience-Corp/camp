package workitem

import "testing"

func TestLinkWorkitemID(t *testing.T) {
	tests := []struct {
		name string
		wi   *WorkItem
		want string
	}{
		{"nil", nil, ""},
		{
			"adopted workitem uses stable id",
			&WorkItem{StableID: "design-x-2026-07-17", Key: "design:workflow/design/x", WorkflowType: WorkflowTypeDesign},
			"design-x-2026-07-17",
		},
		{
			"festival uses fest.yaml id, never its slash-bearing key",
			&WorkItem{WorkflowType: WorkflowTypeFestival, SourceID: "SC0001", Key: "festival:festivals/planning/x-SC0001"},
			"SC0001",
		},
		{
			"id-less non-festival falls back to key",
			&WorkItem{WorkflowType: WorkflowTypeDesign, Key: "design:workflow/design/x"},
			"design:workflow/design/x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LinkWorkitemID(tt.wi); got != tt.want {
				t.Fatalf("LinkWorkitemID() = %q, want %q", got, tt.want)
			}
		})
	}
}
