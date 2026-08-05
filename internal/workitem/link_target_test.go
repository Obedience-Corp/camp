package workitem

import "testing"

func TestLinkWorkitemID(t *testing.T) {
	tests := []struct {
		name string
		wi   *WorkItem
		want string
	}{
		{"nil", nil, ""},
		{
			"adopted workitem uses stable id",
			&WorkItem{StableID: "design-x-2026-07-17", Key: "design:workflow/design/x", WorkflowType: WorkflowTypeDesign},
			"design-x-2026-07-17",
		},
		{
			"festival uses fest.yaml id, never its slash-bearing key",
			&WorkItem{WorkflowType: WorkflowTypeFestival, SourceID: "SC0001", Key: "festival:festivals/planning/x-SC0001"},
			"SC0001",
		},
		{
			"intent uses its frontmatter id, never its slash-bearing key",
			&WorkItem{
				WorkflowType: WorkflowTypeIntent,
				SourceID:     "fest-phase-gate-templates-give-20260727-123326",
				Key:          "intent:.campaign/intents/active/fest-phase-gate-templates-give-20260727-123326.md",
			},
			"fest-phase-gate-templates-give-20260727-123326",
		},
		{
			"intent with no frontmatter id falls back to key",
			&WorkItem{WorkflowType: WorkflowTypeIntent, Key: "intent:.campaign/intents/inbox/x.md"},
			"intent:.campaign/intents/inbox/x.md",
		},
		{
			"stable id wins over a source-declared id",
			&WorkItem{WorkflowType: WorkflowTypeIntent, StableID: "adopted-id", SourceID: "frontmatter-id", Key: "intent:x.md"},
			"adopted-id",
		},
		{
			"id-less non-festival falls back to key",
			&WorkItem{WorkflowType: WorkflowTypeDesign, Key: "design:workflow/design/x"},
			"design:workflow/design/x",
		},
		{
			"design source_id is not an addressable id",
			&WorkItem{WorkflowType: WorkflowTypeDesign, SourceID: "not-addressable", Key: "design:workflow/design/x"},
			"design:workflow/design/x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LinkWorkitemID(tt.wi); got != tt.want {
				t.Fatalf("LinkWorkitemID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSourceDeclaredID(t *testing.T) {
	tests := []struct {
		name string
		wi   *WorkItem
		want string
	}{
		{"nil", nil, ""},
		{"festival", &WorkItem{WorkflowType: WorkflowTypeFestival, SourceID: "SC0001"}, "SC0001"},
		{"intent", &WorkItem{WorkflowType: WorkflowTypeIntent, SourceID: "idea-20260101-000000"}, "idea-20260101-000000"},
		{"design carries no source-declared id", &WorkItem{WorkflowType: WorkflowTypeDesign, SourceID: "x"}, ""},
		{"explore carries no source-declared id", &WorkItem{WorkflowType: WorkflowTypeExplore, SourceID: "x"}, ""},
		{"custom type carries no source-declared id", &WorkItem{WorkflowType: "code_reviews", SourceID: "x"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SourceDeclaredID(tt.wi); got != tt.want {
				t.Fatalf("SourceDeclaredID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLinkMatchesWorkitem(t *testing.T) {
	intent := &WorkItem{
		WorkflowType: WorkflowTypeIntent,
		SourceID:     "idea-20260101-000000",
		Key:          "intent:.campaign/intents/inbox/idea-20260101-000000.md",
	}
	design := &WorkItem{
		WorkflowType: WorkflowTypeDesign,
		StableID:     "design-x-2026-07-17",
		Key:          "design:workflow/design/x",
	}

	tests := []struct {
		name        string
		wi          *WorkItem
		workitemID  string
		workitemKey string
		want        bool
	}{
		{"nil workitem never matches", nil, "anything", "anything", false},
		{"empty link fields never match", design, "", "", false},
		{"unrelated id does not match", design, "design-other", "design:workflow/design/other", false},
		{"key of another workitem does not match", intent, "", "intent:.campaign/intents/inbox/other.md", false},
		{"intent matches its frontmatter id", intent, intent.SourceID, "", true},
		{"intent matches a legacy row keyed by workitem_id", intent, intent.Key, "", true},
		{"intent matches a row carrying only workitem_key", intent, "", intent.Key, true},
		{"design matches its stable id", design, design.StableID, design.Key, true},
		{"design matches a legacy row keyed by workitem_id", design, design.Key, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LinkMatchesWorkitem(tt.wi, tt.workitemID, tt.workitemKey); got != tt.want {
				t.Fatalf("LinkMatchesWorkitem(%q, %q) = %v, want %v",
					tt.workitemID, tt.workitemKey, got, tt.want)
			}
		})
	}
}
