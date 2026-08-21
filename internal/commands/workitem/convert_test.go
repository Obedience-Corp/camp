package workitem

import (
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestConvertRelPath(t *testing.T) {
	cases := []struct {
		name        string
		oldRel      string
		workflowRel string
		newType     string
		wantRel     string
		wantOldType string
		wantErr     bool
	}{
		{
			name:        "explore directory to design",
			oldRel:      "workflow/explore/camp-triage",
			workflowRel: "workflow",
			newType:     "design",
			wantRel:     "workflow/design/camp-triage",
			wantOldType: "explore",
		},
		{
			name:        "design file to explore",
			oldRel:      "workflow/design/note.md",
			workflowRel: "workflow",
			newType:     "explore",
			wantRel:     "workflow/explore/note.md",
			wantOldType: "design",
		},
		{
			name:        "custom type destination",
			oldRel:      "workflow/explore/widget",
			workflowRel: "workflow",
			newType:     "feature",
			wantRel:     "workflow/feature/widget",
			wantOldType: "explore",
		},
		{
			name:        "not under workflow root",
			oldRel:      "docs/camp-triage",
			workflowRel: "workflow",
			newType:     "design",
			wantErr:     true,
		},
		{
			name:        "type root without name",
			oldRel:      "workflow/explore",
			workflowRel: "workflow",
			newType:     "design",
			wantErr:     true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotRel, gotType, err := convertRelPath(c.oldRel, c.workflowRel, c.newType)
			if c.wantErr {
				if err == nil {
					t.Fatalf("convertRelPath(%q) = %q, want error", c.oldRel, gotRel)
				}
				return
			}
			if err != nil {
				t.Fatalf("convertRelPath unexpected error: %v", err)
			}
			if gotRel != c.wantRel || gotType != c.wantOldType {
				t.Fatalf("convertRelPath = (%q, %q), want (%q, %q)", gotRel, gotType, c.wantRel, c.wantOldType)
			}
		})
	}
}

func TestConvertKey(t *testing.T) {
	cases := []struct {
		name    string
		oldKey  string
		oldRel  string
		newRel  string
		newType string
		isFile  bool
		want    string
		wantErr bool
	}{
		{
			name:    "directory key takes new type",
			oldKey:  "explore:workflow/explore/camp-triage",
			oldRel:  "workflow/explore/camp-triage",
			newRel:  "workflow/design/camp-triage",
			newType: "design",
			want:    "design:workflow/design/camp-triage",
		},
		{
			name:    "file key keeps file prefix",
			oldKey:  "file:workflow/explore/note.md",
			oldRel:  "workflow/explore/note.md",
			newRel:  "workflow/design/note.md",
			newType: "design",
			isFile:  true,
			want:    "file:workflow/design/note.md",
		},
		{
			name:    "key must end with path",
			oldKey:  "explore:workflow/explore/other",
			oldRel:  "workflow/explore/camp-triage",
			newRel:  "workflow/design/camp-triage",
			newType: "design",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := convertKey(c.oldKey, c.oldRel, c.newRel, c.newType, c.isFile)
			if c.wantErr {
				if err == nil {
					t.Fatalf("convertKey = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("convertKey unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("convertKey = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEnsureConvertible(t *testing.T) {
	cases := []struct {
		name    string
		item    wkitem.WorkItem
		wantErr bool
	}{
		{"festival rejected", wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeFestival, RelativePath: "festivals/active/x"}, true},
		{"intent rejected", wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeIntent, RelativePath: ".campaign/intents/inbox/x.md"}, true},
		{"dungeon rejected", wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeDesign, RelativePath: "workflow/design/.dungeon/completed/2026-08-04/x"}, true},
		{"legacy dungeon rejected", wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeExplore, RelativePath: "workflow/explore/dungeon/archived/x"}, true},
		{"design allowed", wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeDesign, RelativePath: "workflow/design/timeline"}, false},
		{"explore allowed", wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeExplore, RelativePath: "workflow/explore/notes"}, false},
		{"custom type allowed", wkitem.WorkItem{WorkflowType: wkitem.WorkflowType("feature"), RelativePath: "workflow/feature/widget"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ensureConvertible(&c.item)
			if c.wantErr != (err != nil) {
				t.Fatalf("ensureConvertible(%q) err=%v, wantErr=%v", c.item.RelativePath, err, c.wantErr)
			}
		})
	}
}
