package quest

import (
	"testing"
)

func TestServiceUpdateMetadata(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Metadata Quest", "Original purpose", "Original description", []string{"metadata"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	originalUpdatedAt := created.Quest.UpdatedAt

	purpose := "  Updated purpose  "
	updated, err := svc.UpdateMetadata(ctx, created.Quest.ID, MetadataUpdateOptions{Purpose: &purpose})
	if err != nil {
		t.Fatalf("UpdateMetadata(purpose) error = %v", err)
	}
	if updated.Quest.Purpose != "Updated purpose" {
		t.Fatalf("Purpose = %q, want trimmed update", updated.Quest.Purpose)
	}
	if updated.Quest.Description != "Original description" {
		t.Fatalf("Description = %q, want preserved original", updated.Quest.Description)
	}
	if !updated.Quest.UpdatedAt.After(originalUpdatedAt) {
		t.Fatalf("UpdatedAt = %s, want after %s", updated.Quest.UpdatedAt, originalUpdatedAt)
	}
	if len(updated.Files) != 1 || updated.Files[0] != created.Quest.Path {
		t.Fatalf("Files = %#v, want quest path %q", updated.Files, created.Quest.Path)
	}

	description := ""
	cleared, err := svc.UpdateMetadata(ctx, created.Quest.ID, MetadataUpdateOptions{Description: &description})
	if err != nil {
		t.Fatalf("UpdateMetadata(description clear) error = %v", err)
	}
	if cleared.Quest.Description != "" {
		t.Fatalf("Description = %q, want cleared", cleared.Quest.Description)
	}
	if cleared.Quest.Purpose != "Updated purpose" {
		t.Fatalf("Purpose = %q, want preserved update", cleared.Quest.Purpose)
	}
}

func TestServiceUpdateMetadataRequiresField(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Metadata Quest", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := svc.UpdateMetadata(ctx, created.Quest.ID, MetadataUpdateOptions{}); err == nil {
		t.Fatal("UpdateMetadata() error = nil, want required field error")
	}
}

func TestServiceUpdateMetadataAllowsDefaultQuest(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	// The default quest is editable like any other quest; updating its metadata
	// must succeed.
	purpose := "new default purpose"
	updated, err := svc.UpdateMetadata(ctx, DefaultQuestID, MetadataUpdateOptions{Purpose: &purpose})
	if err != nil {
		t.Fatalf("UpdateMetadata(default) unexpected error: %v", err)
	}
	if updated.Quest.Purpose != "new default purpose" {
		t.Fatalf("Purpose = %q, want %q", updated.Quest.Purpose, "new default purpose")
	}
}

func TestEnrichFromWorkitem_FillsEmptyFields(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Placeholder Quest", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	enriched, err := svc.EnrichFromWorkitem(ctx, created.Quest.ID, WorkitemEnrichment{
		Title:   "Workitem Title",
		Summary: "Workitem summary text",
	})
	if err != nil {
		t.Fatalf("EnrichFromWorkitem() error = %v", err)
	}
	if enriched.Quest.Purpose != "Workitem Title" {
		t.Fatalf("Purpose = %q, want %q", enriched.Quest.Purpose, "Workitem Title")
	}
	if enriched.Quest.Description != "Workitem summary text" {
		t.Fatalf("Description = %q, want %q", enriched.Quest.Description, "Workitem summary text")
	}
	if len(enriched.Files) != 1 || enriched.Files[0] != created.Quest.Path {
		t.Fatalf("Files = %#v, want [%s]", enriched.Files, created.Quest.Path)
	}
}

func TestEnrichFromWorkitem_PreservesUserSuppliedFields(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "User Quest", "User purpose", "User description", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	enriched, err := svc.EnrichFromWorkitem(ctx, created.Quest.ID, WorkitemEnrichment{
		Title:   "Workitem Title",
		Summary: "Workitem summary",
	})
	if err != nil {
		t.Fatalf("EnrichFromWorkitem() error = %v", err)
	}
	if enriched.Quest.Purpose != "User purpose" {
		t.Fatalf("Purpose = %q, want %q (user-supplied must not be overwritten)", enriched.Quest.Purpose, "User purpose")
	}
	if enriched.Quest.Description != "User description" {
		t.Fatalf("Description = %q, want %q (user-supplied must not be overwritten)", enriched.Quest.Description, "User description")
	}
	// No write should occur when nothing changed.
	if len(enriched.Files) != 0 {
		t.Fatalf("Files = %#v, want empty (nothing changed)", enriched.Files)
	}
}

func TestEnrichFromWorkitem_PartialFill(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	// Purpose supplied by user, Description left empty.
	created, err := svc.Create(ctx, "Partial Quest", "My purpose", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	enriched, err := svc.EnrichFromWorkitem(ctx, created.Quest.ID, WorkitemEnrichment{
		Title:   "Workitem Title",
		Summary: "Workitem summary",
	})
	if err != nil {
		t.Fatalf("EnrichFromWorkitem() error = %v", err)
	}
	if enriched.Quest.Purpose != "My purpose" {
		t.Fatalf("Purpose = %q, want %q (preserved)", enriched.Quest.Purpose, "My purpose")
	}
	if enriched.Quest.Description != "Workitem summary" {
		t.Fatalf("Description = %q, want %q (enriched)", enriched.Quest.Description, "Workitem summary")
	}
}

func TestEnrichFromWorkitem_NoEnrichmentData(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Empty Quest", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Empty enrichment data: no-op.
	enriched, err := svc.EnrichFromWorkitem(ctx, created.Quest.ID, WorkitemEnrichment{})
	if err != nil {
		t.Fatalf("EnrichFromWorkitem() error = %v", err)
	}
	if enriched.Quest.Purpose != "" {
		t.Fatalf("Purpose = %q, want empty", enriched.Quest.Purpose)
	}
	if enriched.Quest.Description != "" {
		t.Fatalf("Description = %q, want empty", enriched.Quest.Description)
	}
	if len(enriched.Files) != 0 {
		t.Fatalf("Files = %#v, want empty (no enrichment data)", enriched.Files)
	}
}

func TestEnrichFromWorkitem_BothAlreadySet(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Full Quest", "Existing", "Existing desc", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	enriched, err := svc.EnrichFromWorkitem(ctx, created.Quest.ID, WorkitemEnrichment{
		Title:   "Should Not Apply",
		Summary: "Should Not Apply",
	})
	if err != nil {
		t.Fatalf("EnrichFromWorkitem() error = %v", err)
	}
	if enriched.Quest.Purpose != "Existing" {
		t.Fatalf("Purpose = %q, want %q", enriched.Quest.Purpose, "Existing")
	}
	if enriched.Quest.Description != "Existing desc" {
		t.Fatalf("Description = %q, want %q", enriched.Quest.Description, "Existing desc")
	}
	if len(enriched.Files) != 0 {
		t.Fatalf("Files = %#v, want empty (both fields already set)", enriched.Files)
	}
}
