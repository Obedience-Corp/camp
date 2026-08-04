package intent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	intentcore "github.com/Obedience-Corp/camp/internal/intent"
)

func TestOutputNoteFoldersPayload_EmptyIsArrayNotNull(t *testing.T) {
	var buf bytes.Buffer
	if err := outputNoteFoldersPayload(&buf, "/tmp/campaign", nil); err != nil {
		t.Fatalf("outputNoteFoldersPayload: %v", err)
	}
	raw := buf.String()
	if strings.Contains(raw, `"folders": null`) {
		t.Fatalf("folders must marshal as [], not null:\n%s", raw)
	}
	if !strings.Contains(raw, `"folders": []`) && !strings.Contains(raw, `"folders":[]`) {
		// indented form
		if !strings.Contains(raw, `"folders": [`) {
			t.Fatalf("expected folders array:\n%s", raw)
		}
	}

	var payload NoteFoldersPayload
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.SchemaVersion != NoteFoldersJSONVersion {
		t.Errorf("schema_version = %q, want %q", payload.SchemaVersion, NoteFoldersJSONVersion)
	}
	if payload.Folders == nil {
		t.Fatal("Folders should be non-nil empty slice after unmarshal of []")
	}
	if len(payload.Folders) != 0 {
		t.Fatalf("Folders len = %d, want 0", len(payload.Folders))
	}
}

func TestOutputNoteFoldersPayload_FieldCasing(t *testing.T) {
	var buf bytes.Buffer
	folders := []intentcore.NoteFolder{
		{Status: intentcore.StatusNote, Name: "Notes", Depth: 0, Reserved: false, Count: 2},
		{Status: intentcore.StatusNoteMeetings, Name: "Meetings", Depth: 1, Reserved: true, Count: 1},
	}
	if err := outputNoteFoldersPayload(&buf, "/tmp/campaign", folders); err != nil {
		t.Fatalf("outputNoteFoldersPayload: %v", err)
	}
	raw := buf.String()
	for _, want := range []string{
		`"schema_version"`,
		`"generated_at"`,
		`"campaign_root"`,
		`"folders"`,
		`"status"`,
		`"name"`,
		`"depth"`,
		`"reserved"`,
		`"count"`,
		NoteFoldersJSONVersion,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("JSON missing %s:\n%s", want, raw)
		}
	}
}
