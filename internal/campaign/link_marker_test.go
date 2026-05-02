package campaign

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLinkMarker_V3_RoundTrip is part of the non-project-symlinks design.
// It locks in the V3 marker schema where attachments are distinguished from
// linked projects by an explicit Kind field. This test will start passing
// once Kind is added to LinkMarker and WriteMarker stamps Version 3.
func TestLinkMarker_V3_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	in := LinkMarker{
		Version:          3,
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

	if got.Version != 3 {
		t.Errorf("Version = %d, want 3", got.Version)
	}
	if got.Kind != "attachment" {
		t.Errorf("Kind = %q, want %q", got.Kind, "attachment")
	}
	if got.ActiveCampaignID != "campaign-xyz" {
		t.Errorf("ActiveCampaignID = %q, want %q", got.ActiveCampaignID, "campaign-xyz")
	}
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
