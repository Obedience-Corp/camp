package workitem

import (
	"errors"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestParseCompletionDecision(t *testing.T) {
	for _, value := range []completionDecision{completionReview, completionAcknowledge, completionRecurring} {
		got, err := parseCompletionDecision("  " + string(value) + "  ")
		if err != nil || got != value {
			t.Fatalf("parseCompletionDecision(%q) = %q, %v", value, got, err)
		}
	}
	_, err := parseCompletionDecision("never")
	var validationErr *camperrors.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "completion" {
		t.Fatalf("invalid decision error = %v", err)
	}
}

func TestApplyCompletionDecision(t *testing.T) {
	completed := &wkitem.WorkItemWorkflow{LatestRunID: "run-002", LatestRunStatus: "completed"}
	tests := []struct {
		name       string
		meta       *wkitem.Metadata
		decision   completionDecision
		workflow   *wkitem.WorkItemWorkflow
		want       completionSnapshot
		wantChange bool
		wantErr    bool
	}{
		{name: "acknowledge latest", meta: &wkitem.Metadata{}, decision: completionAcknowledge, workflow: completed,
			want: completionSnapshot{Policy: wkitem.CompletionPolicyReview, ReviewedRunID: "run-002"}, wantChange: true},
		{name: "acknowledge exact run no-op", meta: &wkitem.Metadata{CompletionReviewedRunID: "run-002"}, decision: completionAcknowledge, workflow: completed,
			want: completionSnapshot{Policy: wkitem.CompletionPolicyReview, ReviewedRunID: "run-002"}},
		{name: "acknowledge requires completion", meta: &wkitem.Metadata{}, decision: completionAcknowledge, workflow: &wkitem.WorkItemWorkflow{ActiveRunID: "run-003"}, wantErr: true},
		{name: "recurring clears reviewed", meta: &wkitem.Metadata{CompletionReviewedRunID: "run-001"}, decision: completionRecurring,
			want: completionSnapshot{Policy: wkitem.CompletionPolicyRecurring}, wantChange: true},
		{name: "review clears recurring", meta: &wkitem.Metadata{CompletionPolicy: wkitem.CompletionPolicyRecurring}, decision: completionReview,
			want: completionSnapshot{Policy: wkitem.CompletionPolicyReview}, wantChange: true},
		{name: "default review no-op", meta: &wkitem.Metadata{}, decision: completionReview,
			want: completionSnapshot{Policy: wkitem.CompletionPolicyReview}},
		{name: "explicit review canonicalized", meta: &wkitem.Metadata{CompletionPolicy: wkitem.CompletionPolicyReview}, decision: completionReview,
			want: completionSnapshot{Policy: wkitem.CompletionPolicyReview}, wantChange: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, after, changed, err := applyCompletionDecision(tc.meta, tc.decision, tc.workflow)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("applyCompletionDecision: %v", err)
			}
			if after != tc.want || changed != tc.wantChange {
				t.Fatalf("after/change = %+v/%v, want %+v/%v", after, changed, tc.want, tc.wantChange)
			}
		})
	}
}
