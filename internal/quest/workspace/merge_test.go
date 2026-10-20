package workspace

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergePush_AppendPreservesUserTabs(t *testing.T) {
	var spec PushSpec
	if err := json.Unmarshal(fixture(t, "push-spec-valid.json"), &spec); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePushSpec("", &spec); err != nil {
		t.Fatal(err)
	}
	got, err := MergePush(fixture(t, "valid-layout.json"), &spec)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "appended" {
		t.Fatalf("action=%s", got.Action)
	}
	var doc map[string]any
	if err := json.Unmarshal(got.Document, &doc); err != nil {
		t.Fatal(err)
	}
	tabs := doc["workspaceTabs"].([]any)
	if len(tabs) != 2 {
		t.Fatalf("tabs=%d want 2", len(tabs))
	}
	ids := []string{}
	for _, tab := range tabs {
		ids = append(ids, tab.(map[string]any)["id"].(string))
	}
	if ids[0] != "workspace-tab-1" || ids[1] != "agent-review" {
		t.Fatalf("ids=%v", ids)
	}
	agent := tabs[1].(map[string]any)
	if agent["origin"] != OriginAgent {
		t.Fatalf("origin=%v", agent["origin"])
	}
}

func TestMergePush_IdempotentReplaceAgentTab(t *testing.T) {
	var spec PushSpec
	if err := json.Unmarshal(fixture(t, "push-spec-valid.json"), &spec); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePushSpec("", &spec); err != nil {
		t.Fatal(err)
	}
	first, err := MergePush(fixture(t, "agent-metadata.json"), &spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.Action != "replaced" {
		t.Fatalf("action=%s", first.Action)
	}
	second, err := MergePush(first.Document, &spec)
	if err != nil {
		t.Fatal(err)
	}
	if second.Action != "replaced" {
		t.Fatalf("second action=%s", second.Action)
	}
	var doc map[string]any
	if err := json.Unmarshal(second.Document, &doc); err != nil {
		t.Fatal(err)
	}
	tabs := doc["workspaceTabs"].([]any)
	if len(tabs) != 1 {
		t.Fatalf("duplicate tabs: %d", len(tabs))
	}
	// Unknown metadata from the previous agent tab must survive replace.
	if tabs[0].(map[string]any)["customAgentNote"] != "keep-me" {
		t.Fatalf("lost unknown metadata: %#v", tabs[0])
	}
}

func TestMergePush_RejectsUserOwnedID(t *testing.T) {
	spec := PushSpec{
		ID:       "user-main",
		Title:    "Hijack",
		PaneTree: json.RawMessage(`{"id":"p","type":"pane","content":{"type":"empty"}}`),
	}
	if err := ValidatePushSpec("", &spec); err != nil {
		t.Fatal(err)
	}
	_, err := MergePush(fixture(t, "conflicting-ownership.json"), &spec)
	if err == nil {
		t.Fatal("expected user-owned rejection")
	}
	if !strings.Contains(err.Error(), "user-owned") {
		t.Fatalf("error=%v", err)
	}

	spec.ID = "untagged"
	_, err = MergePush(fixture(t, "conflicting-ownership.json"), &spec)
	if err == nil {
		t.Fatal("expected untagged (user) rejection")
	}
}

func TestMergePush_EmptyDocument(t *testing.T) {
	spec := PushSpec{
		ID:       "agent-a",
		Title:    "A",
		PaneTree: json.RawMessage(`{"id":"p","type":"pane","content":{"type":"empty"}}`),
	}
	if err := ValidatePushSpec("", &spec); err != nil {
		t.Fatal(err)
	}
	got, err := MergePush(nil, &spec)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "appended" {
		t.Fatalf("action=%s", got.Action)
	}
	if !strings.Contains(string(got.Document), `"version": 2`) {
		t.Fatalf("document=%s", got.Document)
	}
}

func TestMergePush_MigratesLegacyRootLayoutIntoUserTab(t *testing.T) {
	legacy := []byte(`{
  "version": 2,
  "paneTree": {"id":"pane-1","type":"pane","content":{"type":"file","filePath":"docs/README.md"}},
  "openFiles": [{"path":"docs/README.md","name":"README.md"}],
  "currentPath": "docs/README.md",
  "activePaneId": "pane-1"
}`)
	if err := ValidateDocument("", legacy); err != nil {
		t.Fatal(err)
	}
	spec := PushSpec{
		ID:       "agent-a",
		Title:    "Agent",
		PaneTree: json.RawMessage(`{"id":"p","type":"pane","content":{"type":"empty"}}`),
	}
	if err := ValidatePushSpec("", &spec); err != nil {
		t.Fatal(err)
	}
	got, err := MergePush(legacy, &spec)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(got.Document, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["paneTree"]; ok {
		t.Fatalf("root paneTree should be cleared after migration: %s", got.Document)
	}
	var tabs []map[string]json.RawMessage
	if err := json.Unmarshal(doc["workspaceTabs"], &tabs); err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 2 {
		t.Fatalf("want migrated user tab + agent tab, got %d: %s", len(tabs), got.Document)
	}
	var userID, agentID string
	_ = json.Unmarshal(tabs[0]["id"], &userID)
	_ = json.Unmarshal(tabs[1]["id"], &agentID)
	if userID != DefaultUserTabID {
		t.Fatalf("migrated user tab id=%q", userID)
	}
	if agentID != "agent-a" {
		t.Fatalf("agent tab id=%q", agentID)
	}
	if _, ok := tabs[0]["origin"]; ok {
		t.Fatal("migrated legacy tab must not stamp origin=agent")
	}
	if !strings.Contains(string(tabs[0]["paneTree"]), "docs/README.md") {
		t.Fatalf("legacy paneTree lost: %s", tabs[0]["paneTree"])
	}
}

func TestMergePush_MigratesEmptyWorkspaceTabsWithRootLayout(t *testing.T) {
	assertLegacyRootMigrates(t, []byte(`{
  "version": 2,
  "workspaceTabs": [],
  "paneTree": {"id":"pane-1","type":"pane","content":{"type":"file","filePath":"docs/README.md"}},
  "openFiles": [{"path":"docs/README.md","name":"README.md"}],
  "currentPath": "docs/README.md",
  "activePaneId": "pane-1",
  "activeWorkspaceTabId": "workspace-tab-1",
  "customDocNote": "keep-doc"
}`))
}

func TestMergePush_MigratesNullWorkspaceTabsWithRootLayout(t *testing.T) {
	assertLegacyRootMigrates(t, []byte(`{
  "version": 2,
  "workspaceTabs": null,
  "paneTree": {"id":"pane-1","type":"pane","content":{"type":"file","filePath":"docs/README.md"}},
  "openFiles": [{"path":"docs/README.md","name":"README.md"}],
  "currentPath": "docs/README.md",
  "activePaneId": "pane-1",
  "activeWorkspaceTabId": "workspace-tab-1",
  "customDocNote": "keep-doc"
}`))
}

func TestMergePush_EmptyModernLayoutDoesNotInventUserTab(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "empty array", raw: []byte(`{"version":2,"workspaceTabs":[]}`)},
		{name: "null tabs", raw: []byte(`{"version":2,"workspaceTabs":null}`)},
		{name: "empty openFiles only", raw: []byte(`{"version":2,"workspaceTabs":[],"openFiles":[]}`)},
		{name: "null root fields", raw: []byte(`{
  "version": 2,
  "workspaceTabs": [],
  "paneTree": null,
  "openFiles": null,
  "currentPath": null,
  "activePaneId": null,
  "chainConfig": null
}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := PushSpec{
				ID:       "agent-a",
				Title:    "Agent",
				PaneTree: json.RawMessage(`{"id":"p","type":"pane","content":{"type":"empty"}}`),
			}
			if err := ValidatePushSpec("", &spec); err != nil {
				t.Fatal(err)
			}
			got, err := MergePush(tc.raw, &spec)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]json.RawMessage
			if err := json.Unmarshal(got.Document, &doc); err != nil {
				t.Fatal(err)
			}
			var tabs []map[string]json.RawMessage
			if err := json.Unmarshal(doc["workspaceTabs"], &tabs); err != nil {
				t.Fatal(err)
			}
			if len(tabs) != 1 {
				t.Fatalf("invented user tab for empty modern layout: %s", got.Document)
			}
			var id string
			_ = json.Unmarshal(tabs[0]["id"], &id)
			if id != "agent-a" {
				t.Fatalf("expected only agent tab, got %q in %s", id, got.Document)
			}
		})
	}
}

func assertLegacyRootMigrates(t *testing.T, legacy []byte) {
	t.Helper()
	spec := PushSpec{
		ID:       "agent-a",
		Title:    "Agent",
		PaneTree: json.RawMessage(`{"id":"p","type":"pane","content":{"type":"empty"}}`),
	}
	if err := ValidatePushSpec("", &spec); err != nil {
		t.Fatal(err)
	}
	got, err := MergePush(legacy, &spec)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(got.Document, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["paneTree"]; ok {
		t.Fatalf("root paneTree should be cleared after migration: %s", got.Document)
	}
	if note := string(doc["customDocNote"]); !strings.Contains(note, "keep-doc") {
		t.Fatalf("document metadata lost: %s", got.Document)
	}
	var active string
	if err := json.Unmarshal(doc["activeWorkspaceTabId"], &active); err != nil {
		t.Fatal(err)
	}
	if active != DefaultUserTabID {
		t.Fatalf("activeWorkspaceTabId=%q want %q", active, DefaultUserTabID)
	}
	var tabs []map[string]json.RawMessage
	if err := json.Unmarshal(doc["workspaceTabs"], &tabs); err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 2 {
		t.Fatalf("want migrated user tab + agent tab, got %d: %s", len(tabs), got.Document)
	}
	var userID, agentID string
	_ = json.Unmarshal(tabs[0]["id"], &userID)
	_ = json.Unmarshal(tabs[1]["id"], &agentID)
	if userID != DefaultUserTabID {
		t.Fatalf("migrated user tab id=%q", userID)
	}
	if agentID != "agent-a" {
		t.Fatalf("agent tab id=%q", agentID)
	}
	if _, ok := tabs[0]["origin"]; ok {
		t.Fatal("migrated legacy tab must not stamp origin=agent")
	}
	if !strings.Contains(string(tabs[0]["paneTree"]), "docs/README.md") {
		t.Fatalf("legacy paneTree lost: %s", tabs[0]["paneTree"])
	}
}

func TestGenerationStable(t *testing.T) {
	a := Generation([]byte(`{"version":2}`))
	b := Generation([]byte(`{"version":2}`))
	if a != b || len(a) != 64 {
		t.Fatalf("gen=%s/%s", a, b)
	}
	if Generation([]byte(`{"version":3}`)) == a {
		t.Fatal("different bytes must differ")
	}
}
