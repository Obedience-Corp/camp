package commitkit_test

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

func TestWorkitemEnv_NilReturnsNil(t *testing.T) {
	if got := commitkit.WorkitemEnv(nil, "/campaign"); got != nil {
		t.Fatalf("WorkitemEnv(nil) = %v, want nil", got)
	}
}

func TestWithCommitAmendEnv(t *testing.T) {
	base := []string{"CAMP_WORKITEM_REF=WI-abcdef"}
	if got := commitkit.WithCommitAmendEnv(base, false); len(got) != 1 || got[0] != base[0] {
		t.Fatalf("WithCommitAmendEnv(amend=false) = %v, want %v", got, base)
	}
	got := commitkit.WithCommitAmendEnv(base, true)
	if value, ok := envValue(got, "CAMP_COMMIT_AMEND"); !ok || value != "1" {
		t.Fatalf("WithCommitAmendEnv(amend=true) = %v, want CAMP_COMMIT_AMEND=1", got)
	}
}

func TestWorkitemEnv_AllFiveBaseVarsPresent(t *testing.T) {
	wi := &workitem.WorkItem{
		StableID:     "design-timeline-2026-05-24",
		WorkflowType: workitem.WorkflowTypeDesign,
		Title:        "Timeline",
		RelativePath: "workflow/design/timeline",
		SourceMetadata: map[string]any{
			"ref": "WI-abcdef",
		},
	}
	got := commitkit.WorkitemEnv(wi, "/campaign")
	want := map[string]string{
		"CAMP_WORKITEM_ID":    "design-timeline-2026-05-24",
		"CAMP_WORKITEM_REF":   "WI-abcdef",
		"CAMP_WORKITEM_TYPE":  "design",
		"CAMP_WORKITEM_TITLE": "Timeline",
		"CAMP_WORKITEM_PATH":  "workflow/design/timeline",
	}
	for k, wantValue := range want {
		gotValue, ok := envValue(got, k)
		if !ok {
			t.Fatalf("missing %s in env: %v", k, got)
		}
		if gotValue != wantValue {
			t.Fatalf("%s = %q, want %q", k, gotValue, wantValue)
		}
	}
	if _, ok := envValue(got, "CAMP_WORKITEM_QUEST_ID"); ok {
		t.Fatalf("CAMP_WORKITEM_QUEST_ID should be absent without quest_id: %v", got)
	}
}

func TestWorkitemEnv_QuestIDPresentWhenSet(t *testing.T) {
	wi := &workitem.WorkItem{
		StableID:     "design-timeline-2026-05-24",
		WorkflowType: workitem.WorkflowTypeDesign,
		Title:        "Timeline",
		RelativePath: "workflow/design/timeline",
		SourceMetadata: map[string]any{
			"ref":      "WI-abcdef",
			"quest_id": "qst_active",
		},
	}
	got := commitkit.WorkitemEnv(wi, "/campaign")
	v, ok := envValue(got, "CAMP_WORKITEM_QUEST_ID")
	if !ok {
		t.Fatalf("expected CAMP_WORKITEM_QUEST_ID, got %v", got)
	}
	if v != "qst_active" {
		t.Fatalf("CAMP_WORKITEM_QUEST_ID = %q, want qst_active", v)
	}
}

func TestWorkitemEnv_EmptyOptionalFieldsRetainKey(t *testing.T) {
	wi := &workitem.WorkItem{
		StableID:     "design-x-2026-05-24",
		WorkflowType: workitem.WorkflowTypeDesign,
		RelativePath: "workflow/design/x",
		// Title intentionally empty
		SourceMetadata: map[string]any{"ref": "WI-aaaaaa"},
	}
	got := commitkit.WorkitemEnv(wi, "/campaign")
	v, ok := envValue(got, "CAMP_WORKITEM_TITLE")
	if !ok || v != "" {
		t.Fatalf("expected CAMP_WORKITEM_TITLE with empty value, got %v", got)
	}
}

func envValue(env []string, key string) (string, bool) {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return e[len(key)+1:], true
		}
	}
	return "", false
}
