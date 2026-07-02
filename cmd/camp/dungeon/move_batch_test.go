package dungeon

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveItemsAndStatus(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		flags      moveFlags
		wantItems  []string
		wantStatus string
		wantErr    bool
	}{
		{
			name:    "empty args",
			args:    nil,
			wantErr: true,
		},
		{
			name:       "single item no status",
			args:       []string{"alpha.md"},
			wantItems:  []string{"alpha.md"},
			wantStatus: "",
		},
		{
			name:       "item and status",
			args:       []string{"alpha.md", "archived"},
			wantItems:  []string{"alpha.md"},
			wantStatus: "archived",
		},
		{
			name:       "batch trailing status",
			args:       []string{"a", "b", "c", "completed"},
			wantItems:  []string{"a", "b", "c"},
			wantStatus: "completed",
		},
		{
			name:       "docs mode keeps all positionals as items",
			args:       []string{"a", "b", "c"},
			flags:      moveFlags{toDocs: "notes", triage: true},
			wantItems:  []string{"a", "b", "c"},
			wantStatus: "",
		},
		{
			name:       "docs mode single item",
			args:       []string{"note.md"},
			flags:      moveFlags{toDocs: "notes", triage: true},
			wantItems:  []string{"note.md"},
			wantStatus: "",
		},
		{
			name:       "workitem single item no status",
			args:       []string{"slug"},
			flags:      moveFlags{workitem: true},
			wantItems:  []string{"slug"},
			wantStatus: "",
		},
		{
			name:       "workitem item and status",
			args:       []string{"slug", "archived"},
			flags:      moveFlags{workitem: true},
			wantItems:  []string{"slug"},
			wantStatus: "archived",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, status, err := resolveItemsAndStatus(tt.args, tt.flags)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveItemsAndStatus() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveItemsAndStatus() unexpected error: %v", err)
			}
			if status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", status, tt.wantStatus)
			}
			if strings.Join(items, ",") != strings.Join(tt.wantItems, ",") {
				t.Fatalf("items = %v, want %v", items, tt.wantItems)
			}
		})
	}
}

func TestValidateMoveModes(t *testing.T) {
	tests := []struct {
		name    string
		flags   moveFlags
		items   []string
		wantErr bool
	}{
		{
			name:    "to-docs requires triage",
			flags:   moveFlags{toDocs: "notes"},
			items:   []string{"a"},
			wantErr: true,
		},
		{
			name:    "workitem with triage",
			flags:   moveFlags{workitem: true, triage: true},
			items:   []string{"a"},
			wantErr: true,
		},
		{
			name:    "workitem with to-docs",
			flags:   moveFlags{workitem: true, toDocs: "notes"},
			items:   []string{"a"},
			wantErr: true,
		},
		{
			name:    "workitem with multiple items",
			flags:   moveFlags{workitem: true},
			items:   []string{"a", "b"},
			wantErr: true,
		},
		{
			name:    "json without dry-run",
			flags:   moveFlags{jsonOut: true},
			items:   []string{"a"},
			wantErr: true,
		},
		{
			name:  "to-docs with triage is valid",
			flags: moveFlags{toDocs: "notes", triage: true},
			items: []string{"a"},
		},
		{
			name:  "workitem single item is valid",
			flags: moveFlags{workitem: true},
			items: []string{"a"},
		},
		{
			name:  "json with dry-run is valid",
			flags: moveFlags{jsonOut: true, dryRun: true},
			items: []string{"a"},
		},
		{
			name:  "plain batch is valid",
			flags: moveFlags{},
			items: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMoveModes(tt.flags, tt.items)
			if tt.wantErr && err == nil {
				t.Fatalf("validateMoveModes() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateMoveModes() unexpected error: %v", err)
			}
		})
	}
}

func TestRenderDryRunJSON(t *testing.T) {
	previews := []movePreview{
		{Item: "alpha.md", SourceRel: "dungeon/alpha.md", DestRel: "dungeon/completed/2026-07-01/alpha.md", Status: "completed", Mode: "status"},
		{Item: "bravo.md", SourceRel: "bravo.md", DestRel: "dungeon/bravo.md", Mode: "triage_root"},
	}

	var buf bytes.Buffer
	if err := renderDryRunJSON(&buf, previews); err != nil {
		t.Fatalf("renderDryRunJSON() error: %v", err)
	}

	var payload struct {
		SchemaVersion string        `json:"schema_version"`
		DryRun        bool          `json:"dry_run"`
		WouldCommit   bool          `json:"would_commit"`
		Count         int           `json:"count"`
		Moves         []movePreview `json:"moves"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if payload.SchemaVersion != "1" || !payload.DryRun || !payload.WouldCommit {
		t.Fatalf("unexpected envelope: %+v", payload)
	}
	if payload.Count != 2 || len(payload.Moves) != 2 {
		t.Fatalf("count = %d, moves = %d, want 2/2", payload.Count, len(payload.Moves))
	}
	if payload.Moves[0].Item != "alpha.md" || payload.Moves[0].Mode != "status" {
		t.Fatalf("unexpected first move: %+v", payload.Moves[0])
	}
	if strings.Contains(buf.String(), "\"status\"") && payload.Moves[1].Status != "" {
		t.Fatalf("triage_root move should omit status, got %q", payload.Moves[1].Status)
	}
}

func TestRenderDryRunJSONEmptyIsEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	if err := renderDryRunJSON(&buf, []movePreview{}); err != nil {
		t.Fatalf("renderDryRunJSON() error: %v", err)
	}
	if !strings.Contains(buf.String(), "\"moves\": []") {
		t.Fatalf("empty plan should marshal moves as [], got: %s", buf.String())
	}
	if strings.Contains(buf.String(), "\"would_commit\": true") {
		t.Fatalf("empty plan should not claim a commit would be created")
	}
}

func TestRenderDryRunText(t *testing.T) {
	previews := []movePreview{
		{Item: "alpha.md", SourceRel: "dungeon/alpha.md", DestRel: "dungeon/completed/2026-07-01/alpha.md", Status: "completed", Mode: "status"},
		{Item: "bravo.md", SourceRel: "bravo.md", DestRel: "dungeon/bravo.md", Mode: "triage_root"},
	}

	var buf bytes.Buffer
	if err := renderDryRunText(&buf, previews); err != nil {
		t.Fatalf("renderDryRunText() error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Dry run: no filesystem changes, no commit.",
		"alpha.md  dungeon/alpha.md → dungeon/completed/2026-07-01/alpha.md  [completed]",
		"bravo.md  bravo.md → dungeon/bravo.md  [dungeon root]",
		"Would move 2 item(s); a commit would be created.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("text output missing %q; got:\n%s", want, out)
		}
	}
}
