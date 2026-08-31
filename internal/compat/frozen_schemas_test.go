package compat

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/clone"
	"github.com/Obedience-Corp/camp/internal/commands/flow"
	campworkflow "github.com/Obedience-Corp/camp/internal/commands/workflow"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// TestPublishedSchemaVersionsAreFrozen pins the version strings agents and
// scripts branch on. A wording change is never a reason to bump one: a consumer
// that pins workitems/v1alpha12 stops reading camp's output the moment the
// string moves, whether or not the payload actually changed.
func TestPublishedSchemaVersionsAreFrozen(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"camp workitem --json", workitem.SchemaVersion, "workitems/v1alpha12"},
		{"workitem link contracts", links.LinksSchemaVersion, "workitem-links/v1alpha1"},
		{"camp workflow --json", campworkflow.JSONSchemaVersion, "workflow/v1"},
		{"camp flow items", flow.WorkflowItemsSchemaVersion, "workflow-items/v1alpha1"},
		{"camp version --json", version.SchemaVersion, "version/v1alpha1"},
		{"quest checklist", quest.ChecklistSchemaV1, "quest-checklist/v1alpha1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("published schema version moved: got %q, want %q (docs/json-contracts.md, docs/terminology.md)", tt.got, tt.want)
			}
		})
	}
}

// TestWorkitemMetadataSchemaIsFrozen pins the marker schema stamped into every
// tracked work item directory on disk. Unlike the JSON surfaces this one is
// persisted, so a bump would strand existing work items.
func TestWorkitemMetadataSchemaIsFrozen(t *testing.T) {
	if workitem.WorkitemSchemaVersion != "v1alpha9" {
		t.Fatalf("workitem marker schema: got %q, want %q", workitem.WorkitemSchemaVersion, "v1alpha9")
	}
}

// TestRegistryFormatVersionIsFrozen keeps the on-disk registry version where it
// is. Bumping it is a migration, not a wording change.
func TestRegistryFormatVersionIsFrozen(t *testing.T) {
	if config.RegistryVersion != 3 {
		t.Fatalf("registry format version: got %d, want 3", config.RegistryVersion)
	}
}

// TestCloneJSONCamelCaseKeysAreFrozen pins the one published surface that uses
// camelCase. It predates the snake_case convention and consumers already read
// it, so it does not get "fixed" alongside a vocabulary change.
func TestCloneJSONCamelCaseKeysAreFrozen(t *testing.T) {
	got := mustJSON(t, clone.JSONRegistration{
		Registered:   true,
		CampaignID:   legacyCampaignID,
		CampaignName: "legacy-campaign",
	})

	for _, key := range []string{"registered", "campaignId", "campaignName"} {
		if _, ok := got[key]; !ok {
			t.Errorf("clone registration output lost the %q key", key)
		}
	}
	for _, key := range []string{"campaign_id", "campaign_name"} {
		if _, ok := got[key]; ok {
			t.Errorf("clone registration output gained %q; the published spelling is camelCase and both would be a second contract", key)
		}
	}
}
