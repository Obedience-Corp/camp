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
