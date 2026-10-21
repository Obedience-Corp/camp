package settings

import (
	"strings"
	"testing"
)

func TestOwnerSchema(t *testing.T) {
	artifactsSchema := SchemaFor("artifacts.yaml")
	if fieldNamed(t, artifactsSchema, "version").Type != "number" {
		t.Fatal("artifacts version missing")
	}
	roots := fieldNamed(t, artifactsSchema, "roots")
	if roots.Type != "array" || roots.Items == nil || len(roots.Items.Path) != 0 {
		t.Fatalf("roots = %+v", roots)
	}
	if fieldNamed(t, roots.Items.Properties, "path").Type != "string" {
		t.Fatal("artifact root path missing")
	}
	policy := fieldNamed(t, roots.Items.Properties, "policy")
	enumHas(t, policy, "always")
	enumHas(t, policy, "on-demand")

	notices := SchemaFor("notices.yaml")
	if fieldNamed(t, notices, "version").Type != "number" {
		t.Fatal("notices version missing")
	}
	dismissed := fieldNamed(t, notices, "dismissed")
	if dismissed.Type != "object" || dismissed.AdditionalProperties == nil || dismissed.AdditionalProperties.Type != "string" || len(dismissed.AdditionalProperties.Path) != 0 {
		t.Fatalf("dismissed = %+v", dismissed)
	}

	leverageSchema := SchemaFor("leverage/config.json")
	enumHas(t, fieldNamed(t, leverageSchema, "cocomo_project_type"), "organic")
	enumHas(t, fieldNamed(t, leverageSchema, "cocomo_project_type"), "semi-detached")
	enumHas(t, fieldNamed(t, leverageSchema, "cocomo_project_type"), "embedded")
	if fieldNamed(t, leverageSchema, "autocommit").Type != "boolean" {
		t.Fatal("autocommit missing")
	}
	if fieldNamed(t, leverageSchema, "project_start").Type != "string" {
		t.Fatal("project_start missing")
	}
	projects := fieldNamed(t, leverageSchema, "projects")
	if projects.AdditionalProperties == nil || len(projects.AdditionalProperties.Path) != 0 {
		t.Fatalf("leverage projects = %+v", projects)
	}
	entry := projects.AdditionalProperties.Properties
	for _, name := range []string{"path", "monorepo_path", "git_repo"} {
		if fieldNamed(t, entry, name).Type != "string" {
			t.Fatalf("leverage %s missing", name)
		}
	}
	if fieldNamed(t, entry, "include").Type != "boolean" || fieldNamed(t, entry, "in_monorepo").Type != "boolean" {
		t.Fatal("leverage project flags missing")
	}
	if fieldNamed(t, entry, "first_commit_override").Type != "string" {
		t.Fatal("first_commit_override missing")
	}

	manifest := SchemaFor("campaign.yaml")
	registry := fieldNamed(t, manifest, "projects")
	for _, name := range []string{"name", "path", "url", "branch"} {
		if !fieldNamed(t, registry.Items.Properties, name).ReadOnly {
			t.Fatalf("project %s must be read only", name)
		}
	}
	if fieldNamed(t, registry.Items.Properties, "shortcuts").ReadOnly {
		t.Fatal("project shortcuts stay editable")
	}
	if SchemaFor("artifacts.yaml") == nil || SchemaFor("notices.yaml") == nil || SchemaFor("leverage/config.json") == nil {
		t.Fatal("known schemas must be present without reading the files")
	}
}

func TestOwnerValidation(t *testing.T) {
	artifactsBody := []byte("# keep\nversion: 1\nroots:\n  - path: media\n    policy: always\ncustom_note: stay\n")
	ok, err := ValidateSource("artifacts.yaml", artifactsBody, nil)
	if err != nil || !ok.Valid() {
		t.Fatalf("artifacts: %v %+v", err, ok.Diagnostics)
	}
	values, _ := ok.Values.(map[string]any)
	if values["custom_note"] != "stay" {
		t.Fatalf("artifacts values = %#v", ok.Values)
	}
	for _, body := range []string{
		"version: 1\nroots:\n  - path: media\n    policy: weekly\n",
		"version: 1\nroots:\n  - path: media\n  - path: media\n",
		"version: 1\nroots:\n  - path: ../outside\n",
		"version: 9\nroots: []\n",
	} {
		result, err := ValidateSource("artifacts.yaml", []byte(body), nil)
		if err != nil || result.Valid() {
			t.Fatalf("artifacts %q should fail: %v %+v", body, err, result.Diagnostics)
		}
	}

	noticesBody := []byte("version: 1\ndismissed:\n  never-synced-abcdef: 2026-01-02T03:04:05Z\nnote: keep\n")
	ok, err = ValidateSource("notices.yaml", noticesBody, nil)
	if err != nil || !ok.Valid() {
		t.Fatalf("notices: %v %+v", err, ok.Diagnostics)
	}
	values, _ = ok.Values.(map[string]any)
	if values["note"] != "keep" {
		t.Fatalf("notices values = %#v", ok.Values)
	}
	if result, err := ValidateSource("notices.yaml", []byte("version: 2\ndismissed: {}\n"), nil); err != nil || result.Valid() {
		t.Fatal("unknown notices version should fail")
	}
	if result, err := ValidateSource("notices.yaml", []byte("version: 1\ndismissed:\n  id: not-a-time\n"), nil); err != nil || result.Valid() {
		t.Fatal("bad dismissal time should fail")
	}

	leverageBody := []byte(`{"actual_people":1,"cocomo_project_type":"organic","projects":{"camp":{"path":"projects/camp","include":true}},"extra":true}`)
	ok, err = ValidateSource("leverage/config.json", leverageBody, nil)
	if err != nil || !ok.Valid() {
		t.Fatalf("leverage: %v %+v", err, ok.Diagnostics)
	}
	values, _ = ok.Values.(map[string]any)
	if values["extra"] != true {
		t.Fatalf("leverage values = %#v", ok.Values)
	}
	for _, body := range []string{
		`{"cocomo_project_type":"invalid"}`,
		`{"actual_people":-1}`,
		`{"projects":{"camp":{"include":true}}}`,
	} {
		result, err := ValidateSource("leverage/config.json", []byte(body), nil)
		if err != nil || result.Valid() {
			t.Fatalf("leverage %s should fail: %v %+v", body, err, result.Diagnostics)
		}
	}
}

func TestOwnerLifecycleIdentity(t *testing.T) {
	const manifest = "id: 00000000-0000-4000-8000-000000000001\nname: fixture\ntype: personal\nprojects:\n  - name: camp\n    path: projects/camp\n    url: git@example.com:org/camp.git\n    branch: main\n    shortcuts:\n      default: .\n"
	ok, err := ValidateSource("campaign.yaml", []byte(manifest), []byte(manifest))
	if err != nil || !ok.Valid() {
		t.Fatalf("unchanged manifest: %v %+v", err, ok.Diagnostics)
	}
	shortcut := strings.Replace(manifest, "default: .", "default: cmd", 1)
	ok, err = ValidateSource("campaign.yaml", []byte(shortcut), []byte(manifest))
	if err != nil || !ok.Valid() {
		t.Fatalf("shortcut edit: %v %+v", err, ok.Diagnostics)
	}
	for _, body := range []string{
		strings.Replace(manifest, "name: camp", "name: other", 1),
		strings.Replace(manifest, "path: projects/camp", "path: projects/other", 1),
		strings.Replace(manifest, "url: git@example.com:org/camp.git", "url: git@example.com:org/other.git", 1),
		strings.Replace(manifest, "branch: main", "branch: dev", 1),
		manifest + "  - name: extra\n    path: projects/extra\n",
	} {
		result, err := ValidateSource("campaign.yaml", []byte(body), []byte(manifest))
		if err != nil || result.Valid() {
			t.Fatalf("lifecycle change should fail: %v %+v\n%s", err, result.Diagnostics, body)
		}
	}

	const brokenManifest = "id: 00000000-0000-4000-8000-000000000001\nname: fixture\ntype: personal\nhooks:\n  commit_message: 1\nprojects:\n  - name: camp\n    path: projects/camp\n    url: git@example.com:org/camp.git\n    branch: main\n    shortcuts: bad\n"
	repaired := []byte(manifest)
	ok, err = ValidateSource("campaign.yaml", repaired, []byte(brokenManifest))
	if err != nil || !ok.Valid() {
		t.Fatalf("manifest repair: %v %+v", err, ok.Diagnostics)
	}
	renamed := strings.Replace(manifest, "name: camp", "name: other", 1)
	bad, err := ValidateSource("campaign.yaml", []byte(renamed), []byte(brokenManifest))
	if err != nil || bad.Valid() || !diagHas(bad.Diagnostics, SeverityError, "projects.0.name") {
		t.Fatalf("malformed previous project change: %v %+v", err, bad.Diagnostics)
	}
	changedID := strings.Replace(manifest, "00000000-0000-4000-8000-000000000001", "other", 1)
	bad, err = ValidateSource("campaign.yaml", []byte(changedID), []byte(brokenManifest))
	if err != nil || bad.Valid() || !diagHas(bad.Diagnostics, SeverityError, "id") {
		t.Fatalf("malformed previous id change: %v %+v", err, bad.Diagnostics)
	}
	ok, err = ValidateSource("campaign.yaml", repaired, []byte(":\n  -\n"))
	if err != nil || !ok.Valid() || !diagHas(ok.Diagnostics, SeverityWarning, "") {
		t.Fatalf("unparseable manifest repair: %v %+v", err, ok.Diagnostics)
	}

	const intentDoc = "---\nid: demo-20260119-153412\ntitle: Example Intent\nstatus: inbox\ncreated_at: 2026-01-19\n---\n\nbody\n"
	const brokenIntent = "---\nid: demo-20260119-153412\ntitle: Example Intent\nstatus: inbox\ncreated_at: 2026-01-19\ntags: bad\n---\n\nbody\n"
	ok, err = ValidateSource("intents/inbox/demo.md", []byte(intentDoc), []byte(brokenIntent))
	if err != nil || !ok.Valid() {
		t.Fatalf("intent repair: %v %+v", err, ok.Diagnostics)
	}
	moved := strings.Replace(intentDoc, "status: inbox", "status: active", 1)
	bad, err = ValidateSource("intents/inbox/demo.md", []byte(moved), []byte(brokenIntent))
	if err != nil || bad.Valid() || !diagHas(bad.Diagnostics, SeverityError, "status") {
		t.Fatalf("malformed previous status change: %v %+v", err, bad.Diagnostics)
	}
	ok, err = ValidateSource("intents/inbox/demo.md", []byte(intentDoc), []byte("not frontmatter\n"))
	if err != nil || !ok.Valid() || !diagHas(ok.Diagnostics, SeverityWarning, "") {
		t.Fatalf("unparseable intent repair: %v %+v", err, ok.Diagnostics)
	}

	const questDoc = "id: qst_demo\nname: Demo\nstatus: open\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n"
	const brokenQuest = questDoc + "links: bad\n"
	ok, err = ValidateSource("quests/demo/quest.yaml", []byte(questDoc), []byte(brokenQuest))
	if err != nil || !ok.Valid() {
		t.Fatalf("quest repair: %v %+v", err, ok.Diagnostics)
	}
	paused := strings.Replace(questDoc, "status: open", "status: paused", 1)
	bad, err = ValidateSource("quests/demo/quest.yaml", []byte(paused), []byte(brokenQuest))
	if err != nil || bad.Valid() || !diagHas(bad.Diagnostics, SeverityError, "status") {
		t.Fatalf("malformed previous quest status: %v %+v", err, bad.Diagnostics)
	}
	ok, err = ValidateSource("quests/demo/quest.yaml", []byte(questDoc), []byte(":\n"))
	if err != nil || !ok.Valid() || !diagHas(ok.Diagnostics, SeverityWarning, "") {
		t.Fatalf("unparseable quest repair: %v %+v", err, ok.Diagnostics)
	}
}

func TestOwnerDocumentPaths(t *testing.T) {
	guide := []byte("# OBEY\n\nAuthored guide.\n")
	for _, rel := range []string{"intents/README.md", "intents/OBEY.md", "intents/custom/topic.md"} {
		result, err := ValidateSource(rel, guide, nil)
		if err != nil || !result.Valid() {
			t.Fatalf("%s should be authored text: %v %+v", rel, err, result.Diagnostics)
		}
	}
	broken := []byte("# no frontmatter\n")
	for _, rel := range []string{"intents/inbox/demo.md", "intents/.dungeon/done/demo.md", "intents/dungeon/killed/demo.md", "intents/notes/reading/papers/demo.md"} {
		result, err := ValidateSource(rel, broken, nil)
		if err != nil || result.Valid() {
			t.Fatalf("%s should stay an owner record: %v %+v", rel, err, result.Diagnostics)
		}
	}
	readme, err := ValidateSource("quests/demo/README.md", []byte("# support\n"), nil)
	if err != nil || !readme.Valid() {
		t.Fatalf("quest readme: %v %+v", err, readme.Diagnostics)
	}
	if result, err := ValidateSource("notes/plain.md", []byte("a\x00b"), nil); err != nil || result.Valid() {
		t.Fatalf("nul source: %v %+v", err, result.Diagnostics)
	}

	if !isIntentDocument("intents/.dungeon/archived/idea.md") || !isIntentDocument("intents/dungeon/someday/idea.md") {
		t.Fatal("dungeon spellings must be intent records")
	}
	if !isIntentDocument("intents/notes/idea.md") || !isIntentDocument("intents/notes/meetings/standup.md") {
		t.Fatal("notes tree must be owner records")
	}
	if isIntentDocument("intents/README.md") || isIntentDocument("intents/OBEY.md") || isIntentDocument("intents/custom/topic.md") {
		t.Fatal("root guides and custom folders are authored text")
	}
	if !isQuestDocument("quests/demo/quest.yaml") || !isQuestDocument("quests/.dungeon/completed/demo/quest.yaml") {
		t.Fatal("quest metadata must match quest.yaml inside quests/")
	}
	if isQuestDocument("quests/demo/README.md") || isQuestDocument("quest.yaml") {
		t.Fatal("quest predicate must stay inside the quests tree")
	}
	if !isQuestWorkspaceState("quests/demo/ui-state.json") || !isQuestWorkspaceState("quests/dungeon/archived/demo/ui-state.json") {
		t.Fatal("ui-state.json inside quests/ is workspace state")
	}
	if isQuestWorkspaceState("quests/demo/quest.yaml") || isQuestWorkspaceState("ui-state.json") {
		t.Fatal("workspace state predicate must stay inside the quests tree")
	}
}

func enumHas(t *testing.T, field Field, want string) {
	t.Helper()
	for _, item := range field.Enum {
		if item == want {
			return
		}
	}
	t.Fatalf("enum %v missing %s", field.Enum, want)
}

func diagHas(diags []Diagnostic, severity, path string) bool {
	for _, diag := range diags {
		if diag.Severity == severity && (path == "" || diag.Path == path) {
			return true
		}
	}
	return false
}
