package campaign

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLinkMarker_V4_RoundTrip locks in the shared attachment marker schema
// where attachments are distinguished from linked projects and can carry
// multiple campaign bindings.
func TestLinkMarker_V4_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	in := LinkMarker{
		Version:          LinkMarkerVersion,
		Kind:             "attachment",
		ActiveCampaignID: "campaign-xyz",
	}
	if err := WriteMarker(dir, in); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	got, err := ReadMarker(dir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}

	if got.Version != LinkMarkerVersion {
		t.Errorf("Version = %d, want %d", got.Version, LinkMarkerVersion)
	}
	if got.Kind != "attachment" {
		t.Errorf("Kind = %q, want %q", got.Kind, "attachment")
	}
	if got.ActiveCampaignID != "campaign-xyz" {
		t.Errorf("ActiveCampaignID = %q, want %q", got.ActiveCampaignID, "campaign-xyz")
	}
}

func TestLinkMarker_SharedAttachmentCampaigns(t *testing.T) {
	dir := t.TempDir()

	marker := LinkMarker{
		Version:          LinkMarkerVersion,
		Kind:             KindAttachment,
		ActiveCampaignID: "campaign-a",
	}
	if !marker.AddCampaign("campaign-b") {
		t.Fatal("AddCampaign returned false for a new campaign")
	}
	if marker.AddCampaign("campaign-a") {
		t.Fatal("AddCampaign returned true for an existing campaign")
	}
	if err := WriteMarker(dir, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	got, err := ReadMarker(dir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	wantIDs := []string{"campaign-a", "campaign-b"}
	if gotIDs := got.EffectiveCampaignIDs(); !equalStrings(gotIDs, wantIDs) {
		t.Errorf("EffectiveCampaignIDs = %v, want %v", gotIDs, wantIDs)
	}

	if !got.RemoveCampaign("campaign-a") {
		t.Fatal("RemoveCampaign returned false for a bound campaign")
	}
	if got.ActiveCampaignID != "campaign-b" || len(got.CampaignIDs) != 0 {
		t.Errorf("after RemoveCampaign: active=%q ids=%v, want campaign-b and no additional IDs",
			got.ActiveCampaignID, got.CampaignIDs)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLinkMarker_LegacyDefaultsToProject confirms that markers written before
// Kind existed (v1/v2) are read as Kind="project" so existing linked projects
// keep behaving exactly as before once V3 lands.
func TestLinkMarker_LegacyDefaultsToProject(t *testing.T) {
	dir := t.TempDir()

	// Hand-write a v2 marker (no Kind field, no Version=3).
	legacy := map[string]any{
		"version":            2,
		"active_campaign_id": "camp-1",
		"project_name":       "example",
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LinkMarkerFile), data, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadMarker(dir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}

	if got.Kind != "project" {
		t.Errorf("legacy marker Kind = %q, want %q (default)", got.Kind, "project")
	}
}

// TestLinkMarker_ProjectKindRoundTrip ensures linked-project writes can also
// carry an explicit Kind="project" once the field exists.
func TestLinkMarker_ProjectKindRoundTrip(t *testing.T) {
	dir := t.TempDir()

	in := LinkMarker{
		Version:          3,
		Kind:             "project",
		ActiveCampaignID: "campaign-xyz",
		ProjectName:      "example",
	}
	if err := WriteMarker(dir, in); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	got, err := ReadMarker(dir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}

	if got.Kind != "project" {
		t.Errorf("Kind = %q, want %q", got.Kind, "project")
	}
	if got.ProjectName != "example" {
		t.Errorf("ProjectName = %q, want %q", got.ProjectName, "example")
	}
}
