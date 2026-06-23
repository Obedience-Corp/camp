package quest

import (
	"errors"
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

func TestServiceUpdateMetadataRejectsDefaultQuest(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	purpose := "new default purpose"
	if _, err := svc.UpdateMetadata(ctx, DefaultQuestID, MetadataUpdateOptions{Purpose: &purpose}); !errors.Is(err, ErrDefaultQuestReadOnly) {
		t.Fatalf("UpdateMetadata(default) error = %v, want %v", err, ErrDefaultQuestReadOnly)
	}
}
