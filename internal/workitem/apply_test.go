package workitem

import "testing"

func TestApplyMetadata_AcceptsCustomType(t *testing.T) {
	item := WorkItem{RelativePath: "workflow/incidents/x"}
	md := &Metadata{
		Version: WorkitemSchemaVersion,
		Kind:    MetadataKind,
		ID:      "incident-001",
		Type:    "incident",
		Title:   "Custom type",
	}
	out, err := ApplyMetadata(item, md)
	if err != nil {
		t.Fatalf("ApplyMetadata rejected custom type %q: %v", md.Type, err)
	}
	if out.StableID != "incident-001" {
		t.Errorf("StableID = %q, want incident-001", out.StableID)
	}
	if out.Title != "Custom type" {
		t.Errorf("Title = %q, want metadata override", out.Title)
	}
}

func TestApplyMetadata_ProjectsCompletionState(t *testing.T) {
	item := WorkItem{RelativePath: "workflow/explore/recurring"}
	md := &Metadata{
		Version:                 WorkitemSchemaVersion,
		Kind:                    MetadataKind,
		ID:                      "explore-recurring",
		Type:                    "explore",
		CompletionReviewedRunID: "run-001",
	}
	out, err := ApplyMetadata(item, md)
	if err != nil {
		t.Fatalf("ApplyMetadata: %v", err)
	}
	if out.Completion == nil || out.Completion.Policy != CompletionPolicyReview || out.Completion.ReviewedRunID != "run-001" {
		t.Fatalf("Completion = %+v, want review/run-001", out.Completion)
	}

	md.CompletionReviewedRunID = ""
	md.CompletionPolicy = CompletionPolicyRecurring
	out, err = ApplyMetadata(item, md)
	if err != nil {
		t.Fatalf("ApplyMetadata recurring: %v", err)
	}
	if out.Completion == nil || out.Completion.Policy != CompletionPolicyRecurring {
		t.Fatalf("Completion = %+v, want recurring", out.Completion)
	}
}
