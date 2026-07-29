package tui

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

// TestRenderPreviewWorkflowStatus drives the real preview renderer, not a
// helper: after fest clears active_run_id the terminal state moves from
// run_status to latest_run_status, and the preview must still show a status
// line. Without the fallback a completed workitem rendered its progress steps
// and no status at all.
func TestRenderPreviewWorkflowStatus(t *testing.T) {
	tests := []struct {
		name string
		meta *workitem.WorkItemWorkflow
		want string
	}{
		{
			name: "active run reports run_status",
			meta: &workitem.WorkItemWorkflow{
				WorkflowID: "wf-1", ActiveRunID: "run-001", RunStatus: "active",
				CurrentStep: 1, TotalSteps: 3,
			},
			want: "active",
		},
		{
			name: "completed run reports latest_run_status once active is cleared",
			meta: &workitem.WorkItemWorkflow{
				WorkflowID: "wf-1", LatestRunID: "run-001", LatestRunStatus: "completed",
				CurrentStep: 3, TotalSteps: 3,
			},
			want: "completed",
		},
		{
			name: "latest_run_status alone still renders the workflow block",
			meta: &workitem.WorkItemWorkflow{LatestRunStatus: "abandoned"},
			want: "abandoned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderPreview(workitem.WorkItem{
				Key:          "workflow/design/sweep-me",
				Title:        "sweep me",
				RelativePath: "workflow/design/sweep-me",
				WorkflowMeta: tt.meta,
			}, 80, 24)

			if !strings.Contains(out, "WORKFLOW") {
				t.Fatalf("preview missing WORKFLOW block:\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("preview missing status %q:\n%s", tt.want, out)
			}
		})
	}
}
