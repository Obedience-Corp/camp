package workitem

import (
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func stubGenerators() (func() (string, error), func(string) (string, error), func() string) {
	genID := func() (string, error) { return "design-foo-2026-05-25", nil }
	genRef := func(id string) (string, error) { return "WI-abc123", nil }
	inferTitle := func() string { return "Inferred Title" }
	return genID, genRef, inferTitle
}

func changeFields(changes []repairChange) map[string]repairChange {
	out := make(map[string]repairChange, len(changes))
	for _, c := range changes {
		out[c.Field] = c
	}
	return out
}

func TestComputeRepair_CreateFromMissing(t *testing.T) {
	genID, genRef, inferTitle := stubGenerators()
	plan, err := computeRepair(wkitem.Metadata{}, false, "design", genID, genRef, inferTitle)
	if err != nil {
		t.Fatalf("computeRepair: %v", err)
	}
	if !plan.created {
		t.Error("created should be true when no marker exists")
	}
	if plan.meta.Version != wkitem.WorkitemSchemaVersion {
		t.Errorf("version = %q, want %q", plan.meta.Version, wkitem.WorkitemSchemaVersion)
	}
	if plan.meta.Kind != wkitem.MetadataKind {
		t.Errorf("kind = %q, want %q", plan.meta.Kind, wkitem.MetadataKind)
	}
	if plan.meta.ID != "design-foo-2026-05-25" {
		t.Errorf("id = %q", plan.meta.ID)
	}
	if plan.meta.Type != "design" {
		t.Errorf("type = %q, want design", plan.meta.Type)
	}
	if plan.meta.Ref != "WI-abc123" {
		t.Errorf("ref = %q", plan.meta.Ref)
	}
	if plan.meta.Title != "Inferred Title" {
		t.Errorf("title = %q", plan.meta.Title)
	}
	fields := changeFields(plan.changes)
	for _, want := range []string{"version", "kind", "id", "type", "ref", "title"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("missing change for field %q", want)
		}
	}
}

func TestComputeRepair_LegacyInputsUpgradeToCurrent(t *testing.T) {
	genID, genRef, inferTitle := stubGenerators()
	for _, version := range []string{"v1alpha4", "v1alpha5", "v1alpha6", "v1alpha7"} {
		t.Run(version, func(t *testing.T) {
			current := wkitem.Metadata{
				Version: version,
				Kind:    "workitem",
				ID:      "design-foo-2026-05-25",
				Type:    "design",
				Title:   "Kept Title",
				Ref:     "WI-abc123",
			}
			plan, err := computeRepair(current, true, "design", genID, genRef, inferTitle)
			if err != nil {
				t.Fatalf("computeRepair: %v", err)
			}
			if plan.meta.Version != wkitem.WorkitemSchemaVersion {
				t.Errorf("version = %q, want %q (%s must upgrade to current)", plan.meta.Version, wkitem.WorkitemSchemaVersion, version)
			}
		})
	}
}

func TestComputeRepair_LegacyUpgradeAndMismatch(t *testing.T) {
	genID, genRef, inferTitle := stubGenerators()
	current := wkitem.Metadata{
		Version: "v1alpha5",
		Kind:    "workitem",
		ID:      "design-foo-2026-05-25",
		Type:    "feature",
		Title:   "Kept Title",
	}
	plan, err := computeRepair(current, true, "design", genID, genRef, inferTitle)
	if err != nil {
		t.Fatalf("computeRepair: %v", err)
	}
	if plan.created {
		t.Error("created should be false when a marker exists")
	}
	if plan.meta.Version != wkitem.WorkitemSchemaVersion {
		t.Errorf("version not upgraded: %q", plan.meta.Version)
	}
	if plan.meta.Type != "design" {
		t.Errorf("type not aligned to path: %q", plan.meta.Type)
	}
	if plan.meta.Ref != "WI-abc123" {
		t.Errorf("ref not backfilled: %q", plan.meta.Ref)
	}
	if plan.meta.Title != "Kept Title" {
		t.Errorf("existing title must be preserved, got %q", plan.meta.Title)
	}
	if plan.meta.ID != "design-foo-2026-05-25" {
		t.Errorf("existing id must be preserved, got %q", plan.meta.ID)
	}
	fields := changeFields(plan.changes)
	if _, ok := fields["title"]; ok {
		t.Error("title should not change when already set")
	}
	if c := fields["type"]; c.From != "feature" || c.To != "design" {
		t.Errorf("type change = %+v, want feature->design", c)
	}
}

func TestComputeRepair_Idempotent(t *testing.T) {
	genID, genRef, inferTitle := stubGenerators()
	current := wkitem.Metadata{
		Version: wkitem.WorkitemSchemaVersion,
		Kind:    wkitem.MetadataKind,
		ID:      "design-foo-2026-05-25",
		Type:    "design",
		Title:   "Foo",
		Ref:     "WI-abc123",
	}
	plan, err := computeRepair(current, true, "design", genID, genRef, inferTitle)
	if err != nil {
		t.Fatalf("computeRepair: %v", err)
	}
	if len(plan.changes) != 0 {
		t.Fatalf("valid marker must yield no changes, got %+v", plan.changes)
	}
}

func TestComputeRepair_MissingRefOnly(t *testing.T) {
	genID, genRef, inferTitle := stubGenerators()
	current := wkitem.Metadata{
		Version: wkitem.WorkitemSchemaVersion,
		Kind:    wkitem.MetadataKind,
		ID:      "design-foo-2026-05-25",
		Type:    "design",
		Title:   "Foo",
	}
	plan, err := computeRepair(current, true, "design", genID, genRef, inferTitle)
	if err != nil {
		t.Fatalf("computeRepair: %v", err)
	}
	if len(plan.changes) != 1 || plan.changes[0].Field != "ref" {
		t.Fatalf("want a single ref change, got %+v", plan.changes)
	}
	if plan.meta.Ref != "WI-abc123" {
		t.Errorf("ref = %q", plan.meta.Ref)
	}
}

func TestComputeRepair_SanitizesInvalidQuestID(t *testing.T) {
	genID, genRef, inferTitle := stubGenerators()
	current := wkitem.Metadata{
		Version: wkitem.WorkitemSchemaVersion,
		Kind:    wkitem.MetadataKind,
		ID:      "design-foo-2026-05-25",
		Type:    "design",
		Title:   "Foo",
		Ref:     "WI-abc123",
		QuestID: "not-a-quest-id",
	}
	plan, err := computeRepair(current, true, "design", genID, genRef, inferTitle)
	if err != nil {
		t.Fatalf("computeRepair: %v", err)
	}
	if plan.meta.QuestID != "" {
		t.Errorf("invalid quest_id should be cleared, got %q", plan.meta.QuestID)
	}
	fields := changeFields(plan.changes)
	if c, ok := fields["quest_id"]; !ok || c.Action != repairActionCleared {
		t.Errorf("want quest_id cleared change, got %+v", plan.changes)
	}
}
