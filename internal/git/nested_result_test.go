package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNestedResultsPreserveFailureAndCancellationCounts(t *testing.T) {
	missing := errors.New("repository unavailable")
	children := []NestedResult{
		{Owner: "projects/parent", Path: "projects/parent/child", Attempted: true,
			Initialized: true, Operation: "children", Error: &NestedError{
				Owner: "projects/parent", Path: "projects/parent/child", Op: "children", Cause: missing},
			Children: []NestedResult{{Owner: "projects/parent/child", Path: "projects/parent/child/bad",
				Attempted: true, Operation: "init", Error: &NestedError{Owner: "projects/parent/child",
					Path: "projects/parent/child/bad", Op: "init", Cause: missing}}}},
		{Owner: "projects/parent", Path: "projects/parent/sibling", Skipped: true,
			Operation: "skip", Error: &NestedError{
				Owner: "projects/parent", Path: "projects/parent/sibling", Op: "skip", Cause: context.Canceled}},
	}
	summary := SummarizeNestedResults(children)
	if summary.Attempted != 2 || summary.Skipped != 1 || summary.Initialized != 1 || summary.Failed != 1 {
		t.Fatalf("summary = %+v, want two attempted, one skipped, one initialized, one local failure", summary)
	}
	err := NestedFailures(children)
	if !errors.Is(err, missing) || !errors.Is(err, context.Canceled) {
		t.Fatalf("joined error lost causes: %v", err)
	}
	output := NestedResultOutputs(children)
	if !strings.Contains(output[0].Error, "projects/parent/child") || output[1].Attempted || !output[1].Skipped {
		t.Fatalf("output lost path or skipped state: %+v", output)
	}
}
