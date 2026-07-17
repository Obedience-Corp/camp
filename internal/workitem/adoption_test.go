package workitem

import (
	"strings"
	"testing"
)

func TestNeedsAdoption(t *testing.T) {
	tests := []struct {
		name string
		wi   *WorkItem
		want bool
	}{
		{"nil", nil, false},
		{
			"unadopted design dir",
			&WorkItem{WorkflowType: WorkflowTypeDesign, StableID: ""},
			true,
		},
		{
			"unadopted explore dir",
			&WorkItem{WorkflowType: WorkflowTypeExplore, StableID: ""},
			true,
		},
		{
			"adopted design dir has stable id",
			&WorkItem{WorkflowType: WorkflowTypeDesign, StableID: "design-x-2026-07-17"},
			false,
		},
		{
			"festival is id-less but not adoptable",
			&WorkItem{WorkflowType: WorkflowTypeFestival, StableID: ""},
			false,
		},
		{
			"intent is id-less but not adoptable",
			&WorkItem{WorkflowType: WorkflowTypeIntent, StableID: ""},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsAdoption(tt.wi); got != tt.want {
				t.Fatalf("NeedsAdoption() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotAdoptedError_NamesAdoptCommandAndPath(t *testing.T) {
	err := NotAdoptedError("workflow/design/thing")
	msg := err.Error()
	if !strings.Contains(msg, "workflow/design/thing") {
		t.Errorf("message should name the path: %s", msg)
	}
	if !strings.Contains(msg, "camp workitem adopt workflow/design/thing") {
		t.Errorf("message should name the adopt command: %s", msg)
	}
}
