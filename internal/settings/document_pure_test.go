package settings

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"gopkg.in/yaml.v3"
)

func TestDocumentSchema_ReflectsNestedOwners(t *testing.T) {
	manifest := SchemaFor("campaign.yaml")
	id := fieldNamed(t, manifest, "id")
	if !id.ReadOnly || id.Type != "string" || id.Label != "ID" {
		t.Fatalf("id field = %+v", id)
	}
	concepts := fieldNamed(t, manifest, "concepts")
	if concepts.Type != "array" || concepts.Items == nil || len(concepts.Items.Path) != 0 {
		t.Fatalf("concepts = %+v", concepts)
	}
	if fieldNamed(t, concepts.Items.Properties, "name").Type != "string" {
		t.Fatal("concept name missing")
	}
	children := fieldNamed(t, concepts.Items.Properties, "children")
	if children.Type != "array" || children.Items == nil || fieldNamed(t, children.Items.Properties, "name").Type != "string" {
		t.Fatalf("nested concept children = %+v", children)
	}
	if fieldNamed(t, manifest, "intents").Type != "object" {
		t.Fatal("intents missing")
	}
	workflows := fieldNamed(t, manifest, "workflows")
	if fieldNamed(t, workflows.Properties, "categories").Type != "object" {
		t.Fatal("workflow categories missing")
	}
	hooks := fieldNamed(t, manifest, "hooks")
	message := fieldNamed(t, hooks.Properties, "commit_message")
	if fieldNamed(t, message.Properties, "command").Type != "string" {
		t.Fatal("commit hook command missing")
	}
	if fieldNamed(t, manifest, "created_at").ReadOnly != true {
		t.Fatal("created_at must be read only")
	}

	jumps := SchemaFor("settings/jumps.yaml")
	shortcuts := fieldNamed(t, jumps, "shortcuts")
	if shortcuts.AdditionalProperties == nil || len(shortcuts.AdditionalProperties.Path) != 0 {
		t.Fatalf("shortcuts map descriptor = %+v", shortcuts)
	}
	for _, name := range []string{"command", "workdir", "path"} {
		if fieldNamed(t, shortcuts.AdditionalProperties.Properties, name).Type != "string" {
			t.Fatalf("shortcut %s missing", name)
		}
	}

	fresh := SchemaFor("settings/fresh.yaml")
	follow := fieldNamed(t, fresh, "follow_up")
	if follow.Type != "array" || fieldNamed(t, follow.Items.Properties, "run").Type != "string" {
		t.Fatal("follow_up run missing")
	}
	if fieldNamed(t, fresh, "projects").Type != "object" {
		t.Fatal("fresh projects missing")
	}

	local := SchemaFor("settings/local.json")
	theme := fieldNamed(t, local, "theme_override")
	if theme.Type != "string" || len(theme.Enum) == 0 {
		t.Fatalf("theme = %+v", theme)
	}
	commit := fieldNamed(t, local, "commit")
	if fieldNamed(t, commit.Properties, "sync_project_refs").Type != "boolean" {
		t.Fatal("local commit prefs missing")
	}

	allow := SchemaFor("settings/allowlist.json")
	if fieldNamed(t, allow, "inherit_defaults").Type != "boolean" {
		t.Fatal("inherit_defaults missing")
	}
	commands := fieldNamed(t, allow, "commands")
	if commands.AdditionalProperties == nil || len(commands.AdditionalProperties.Path) != 0 {
		t.Fatalf("commands map descriptor = %+v", commands)
	}
	if fieldNamed(t, commands.AdditionalProperties.Properties, "allowed").Type != "boolean" {
		t.Fatal("allowlist command allowed missing")
	}

	missing, err := Classify("settings/fresh.yaml", DocumentState{Kind: KindMissing})
	if err != nil {
		t.Fatal(err)
	}
	if missing.Exists || !missing.Capabilities.Form || len(missing.Fields) == 0 || !missing.Capabilities.Create {
		t.Fatalf("missing fresh = %+v", missing.Capabilities)
	}
}

func TestDocumentCatalog_Capabilities(t *testing.T) {
	pins, err := Classify("settings/pins.json", DocumentState{Kind: KindFile, Revision: "sha256:abc", CampID: "camp-1"})
	if err != nil {
		t.Fatal(err)
	}
	if pins.Capabilities.SourceEdit || pins.Capabilities.Form {
		t.Fatal("pins must not be raw-editable")
	}
	if !actionSupported(pins.Capabilities.Actions, "pin") || !actionSupported(pins.Capabilities.Actions, "unpin") {
		t.Fatalf("pin actions = %+v", pins.Capabilities.Actions)
	}
	if pins.Capabilities.Reason == "" {
		t.Fatal("managed pins need a reason")
	}

	work, err := Classify("settings/workitems.json", DocumentState{Kind: KindFile})
	if err != nil {
		t.Fatal(err)
	}
	if !actionSupported(work.Capabilities.Actions, "workitem-priority") || !actionSupported(work.Capabilities.Actions, "workitem-group") {
		t.Fatalf("workitem actions = %+v", work.Capabilities.Actions)
	}

	manifest, err := Classify("campaign.yaml", DocumentState{Kind: KindFile})
	if err != nil {
		t.Fatal(err)
	}
	if actionSupported(manifest.Capabilities.Actions, "project-add") {
		t.Fatal("project add must be advertised as unsupported")
	}
	add := actionByID(manifest.Capabilities.Actions, "project-add")
	if add.Supported || len(add.Command) < 3 || add.Command[0] != "camp" || add.Command[1] != "project" {
		t.Fatalf("project-add = %+v", add)
	}

	skill, err := Classify("skills/demo/SKILL.md", DocumentState{Kind: KindFile})
	if err != nil {
		t.Fatal(err)
	}
	if !skill.Capabilities.SourceEdit || !skill.Capabilities.Rename || !skill.Capabilities.Delete {
		t.Fatalf("skill caps = %+v", skill.Capabilities)
	}

	cache, err := Classify("cache/index.json", DocumentState{Kind: KindFile})
	if err != nil {
		t.Fatal(err)
	}
	if cache.Capabilities.SourceEdit || cache.Capabilities.Reason == "" {
		t.Fatalf("cache = %+v", cache.Capabilities)
	}

	link, err := Classify("outside", DocumentState{Kind: KindSymlink, LinkTarget: "../secret"})
	if err != nil {
		t.Fatal(err)
	}
	if link.Format != DocumentSymlink || link.LinkTarget != "../secret" || link.Capabilities.SourceEdit || link.Capabilities.Reason == "" {
		t.Fatalf("symlink = %+v", link)
	}

	if _, err := ResolveRef("../campaign.yaml"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := ResolveRef("/tmp/campaign.yaml"); err == nil {
		t.Fatal("absolute path accepted")
	}
	if _, err := ResolveRef("a\\b"); err == nil {
		t.Fatal("backslash accepted")
	}
	got, err := ResolveRef("campaign_manifest")
	if err != nil || got != "campaign.yaml" {
		t.Fatalf("id resolve = %q %v", got, err)
	}
}

func TestDocumentValidationAndPatch(t *testing.T) {
	const manifest = "id: 00000000-0000-4000-8000-000000000001\nname: fixture\ntype: personal\nmission: Build useful tools\n"
	ok, err := ValidateSource("campaign.yaml", []byte(manifest), nil)
	if err != nil || !ok.Valid() {
		t.Fatalf("fixture manifest: %v %+v", err, ok.Diagnostics)
	}
	changed := strings.Replace(manifest, "00000000-0000-4000-8000-000000000001", "other", 1)
	bad, err := ValidateSource("campaign.yaml", []byte(changed), []byte(manifest))
	if err != nil || bad.Valid() {
		t.Fatal("id change should be rejected")
	}
	prev := config.CampaignConfig{ID: "a", CreatedAt: time.Unix(10, 0).UTC()}
	next := prev
	next.CreatedAt = time.Unix(11, 0).UTC()
	if len(ManifestIdentityDiagnostics(prev, next)) != 1 {
		t.Fatal("created_at change should be reported")
	}

	if result, err := ValidateSource("settings/allowlist.json", []byte(`{"version":99,"commands":{},"inherit_defaults":true}`), nil); err != nil || result.Valid() {
		t.Fatal("unknown allowlist version should fail")
	}
	if result, err := ValidateSource("settings/fresh.yaml", []byte("follow_up:\n  - name: step\n    run: \"\"\n"), nil); err != nil || result.Valid() {
		t.Fatal("empty follow-up run should fail")
	}
	if result, err := ValidateSource("settings/jumps.yaml", []byte("paths:\n  projects: ../outside\n"), nil); err != nil || result.Valid() {
		t.Fatal("jumps traversal should fail")
	}
	if result, err := ValidateSource("notes/broken.yaml", []byte(":\n  -"), nil); err != nil || result.Valid() {
		t.Fatalf("broken yaml should be invalid, err=%v diags=%+v", err, result.Diagnostics)
	}

	raw := []byte("# keep me\nmission: old # keep\ncustom_note: stay\ndescription: gone\nitems:\n  - one\n")
	changes, err := ParsePatch([]byte(`{"changes":[{"op":"set","path":["mission"],"value":"new"},{"op":"set","path":["items"],"value":["two","one"]},{"op":"remove","path":["description"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	patched, err := PatchYAML(raw, changes)
	if err != nil {
		t.Fatal(err)
	}
	text := string(patched)
	for _, want := range []string{"# keep me", "custom_note", "two", "one", "new"} {
		if !strings.Contains(text, want) {
			t.Fatalf("patched yaml missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "description") {
		t.Fatalf("description remained:\n%s", text)
	}
	if strings.Index(text, "two") > strings.Index(text, "one") {
		t.Fatalf("array order lost:\n%s", text)
	}

	nullPatch, err := ParsePatch([]byte(`{"changes":[{"op":"set","path":["mission"],"value":null},{"op":"remove","path":["custom_note"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	nulled, err := PatchYAML(raw, nullPatch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nulled), "null") || strings.Contains(string(nulled), "custom_note") {
		t.Fatalf("null vs remove:\n%s", nulled)
	}

	jsonBody := []byte("{\"theme_override\":\"dark\",\"extra\":true,\"tags\":[\"b\",\"a\"]}\n")
	jsonChanges, err := ParsePatch([]byte(`{"changes":[{"op":"set","path":["theme_override"],"value":null},{"op":"remove","path":["extra"]},{"op":"set","path":["tags"],"value":["a","b"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	jsonOut, err := PatchJSON(jsonBody, jsonChanges)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(jsonOut, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["theme_override"]; !ok || decoded["theme_override"] != nil {
		t.Fatalf("null key = %#v", decoded["theme_override"])
	}
	if _, ok := decoded["extra"]; ok {
		t.Fatal("removed key survived")
	}
	tags, _ := decoded["tags"].([]any)
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("tags = %#v", tags)
	}

	if _, err := ParsePatch([]byte(`{"changes":[{"op":"set","path":["mission"]}]}`)); err == nil {
		t.Fatal("set without value accepted")
	}
	if _, err := ParsePatch([]byte(`{"changes":[{"op":"remove","path":["mission"],"value":1}]}`)); err == nil {
		t.Fatal("remove with value accepted")
	}
}

func TestDocumentReviewFixes(t *testing.T) {
	body, err := os.ReadFile("testdata/documents/map-field.json")
	if err != nil {
		t.Fatal(err)
	}
	var want Field
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatal(err)
	}
	got := fieldNamed(t, SchemaFor("settings/jumps.yaml"), "shortcuts")
	if got.Label != want.Label || got.Type != want.Type || got.AdditionalProperties == nil {
		t.Fatalf("shortcuts = %+v", got)
	}
	if got.AdditionalProperties.Label != want.AdditionalProperties.Label || len(got.AdditionalProperties.Path) != 0 {
		t.Fatalf("shortcut value = %+v", got.AdditionalProperties)
	}
	for _, prop := range want.AdditionalProperties.Properties {
		gotProp := fieldNamed(t, got.AdditionalProperties.Properties, prop.Path[0])
		if gotProp.Label != prop.Label || gotProp.Type != prop.Type {
			t.Fatalf("shortcut %s = %+v", prop.Path[0], gotProp)
		}
	}

	for _, rel := range []string{"scripts/run.py", "scripts/run.sh", "theme.toml", "theme.css", "notes.jsonl", "custom/index.md"} {
		entry, err := Classify(rel, DocumentState{Kind: KindFile})
		if err != nil {
			t.Fatal(err)
		}
		if !entry.Capabilities.SourceEdit || entry.Format == DocumentBinary {
			t.Fatalf("%s should stay authored text: %+v", rel, entry.Capabilities)
		}
	}
	for _, rel := range []string{"state.yaml", "graph.db", "cache/index.json", "events/run.jsonl", "workitems/current.yaml", "graphs/camp.db"} {
		entry, err := Classify(rel, DocumentState{Kind: KindFile})
		if err != nil {
			t.Fatal(err)
		}
		if entry.Capabilities.SourceEdit || entry.Capabilities.Reason == "" {
			t.Fatalf("%s should be managed: %+v", rel, entry.Capabilities)
		}
	}
	for _, rel := range []string{"intents/.intents.jsonl", "intents/crawl.jsonl", "workitems/.workitems.jsonl"} {
		entry, err := Classify(rel, DocumentState{Kind: KindFile})
		if err != nil {
			t.Fatal(err)
		}
		if entry.Capabilities.SourceEdit || entry.Capabilities.Create || entry.Capabilities.Reason == "" {
			t.Fatalf("%s should be an owner audit log: %+v", rel, entry.Capabilities)
		}
		if len(entry.Capabilities.Actions) == 0 || entry.Capabilities.Actions[0].Supported {
			t.Fatalf("%s actions = %+v", rel, entry.Capabilities.Actions)
		}
		cmd := entry.Capabilities.Actions[0].Command
		if len(cmd) < 2 || cmd[0] != "camp" || (cmd[1] != "idea" && cmd[1] != "workitem") {
			t.Fatalf("%s command = %+v", rel, cmd)
		}
	}
	feed, err := Classify("custom/feed.jsonl", DocumentState{Kind: KindFile})
	if err != nil {
		t.Fatal(err)
	}
	if !feed.Capabilities.SourceEdit || feed.Format == DocumentBinary {
		t.Fatalf("custom jsonl = %+v", feed.Capabilities)
	}
	special, err := Classify("fifo", DocumentState{Kind: KindSpecial})
	if err != nil {
		t.Fatal(err)
	}
	if special.Capabilities.Read || special.Capabilities.SourceEdit {
		t.Fatalf("special file = %+v", special.Capabilities)
	}
	binary := ApplyContentFormat(mustEntry(t, "notes.txt"), []byte("ok\x00"))
	if binary.Format != DocumentBinary || binary.Capabilities.SourceEdit {
		t.Fatalf("nul sniff = %+v", binary.Capabilities)
	}

	raw := []byte("count: 1\nflag: false\nnote: keep\n")
	changes, err := ParsePatch([]byte(`{"changes":[{"op":"set","path":["count"],"value":9007199254740993},{"op":"set","path":["flag"],"value":true},{"op":"set","path":["extra"],"value":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	patched, err := PatchYAML(raw, changes)
	if err != nil {
		t.Fatal(err)
	}
	assertYAMLScalar(t, patched, "count", "!!int", "9007199254740993")
	assertYAMLScalar(t, patched, "flag", "!!bool", "true")
	assertYAMLScalar(t, patched, "extra", "!!null", "null")

	header := []byte("# fresh scaffold\n# keep header\n")
	headerChanges, err := ParsePatch([]byte(`{"changes":[{"op":"set","path":["completed_runs"],"value":"prompt"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	headerOut, err := PatchYAML(header, headerChanges)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(headerOut), "# fresh scaffold") || !strings.Contains(string(headerOut), "completed_runs: prompt") {
		t.Fatalf("comment header lost:\n%s", headerOut)
	}

	slash, err := ParsePatch([]byte(`{"changes":[{"op":"set","path":["org/repo"],"value":1},{"op":"set","path":["./scripts/test"],"value":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	slashOut, err := PatchJSON([]byte("{}\n"), slash)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(slashOut, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["org/repo"] != float64(1) || decoded["./scripts/test"] != true {
		t.Fatalf("slash keys = %#v", decoded)
	}
	if _, err := PatchYAML([]byte("base: &base\n  a: 1\nmerged:\n  <<: *base\n"), headerChanges); err == nil {
		t.Fatal("yaml alias patch accepted")
	}

	for _, sample := range []struct {
		rel, body string
	}{
		{"notes/multi.yaml", "a: 1\n---\nb: 2\n"},
		{"notes/dup.yaml", "a: 1\na: 2\n"},
		{"notes/dup.json", "{\"a\":1,\"a\":2}\n"},
		{"notes/trail.json", "{\"a\":1} {\"b\":2}\n"},
	} {
		result, err := ValidateSource(sample.rel, []byte(sample.body), nil)
		if err != nil || result.Valid() {
			t.Fatalf("%s should be invalid: err=%v diags=%+v", sample.rel, err, result.Diagnostics)
		}
	}

	broken := []byte("# no frontmatter\n")
	intentResult, err := ValidateSource("intents/inbox/demo.md", broken, broken)
	if err != nil || intentResult.Valid() {
		t.Fatalf("broken intent accepted: err=%v diags=%+v", err, intentResult.Diagnostics)
	}
	questResult, err := ValidateSource("quests/demo/quest.yaml", []byte(":\n"), []byte(":\n"))
	if err != nil || questResult.Valid() {
		t.Fatalf("broken quest accepted: err=%v diags=%+v", err, questResult.Diagnostics)
	}

	safe, err := JSONSafe(map[any]any{"name": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if named, _ := safe.(map[string]any); named["name"] != "ok" {
		t.Fatalf("json safe = %#v", safe)
	}
	if _, err := JSONSafe(map[any]any{1: "no"}); err == nil {
		t.Fatal("non-string yaml key accepted")
	}

	const matched = "id: camp-1\nname: fixture\ntype: personal\nmission: Build useful tools\n"
	id, brokenID, err := InterpretManifestIdentity([]byte(matched), "camp-1")
	if err != nil || brokenID || id != "camp-1" {
		t.Fatalf("matching identity = %q broken=%v err=%v", id, brokenID, err)
	}
	if _, _, err := InterpretManifestIdentity([]byte("id: other\nname: fixture\n"), "camp-1"); err == nil {
		t.Fatal("mismatched identity accepted")
	}
	if _, _, err := InterpretManifestIdentity([]byte("name: fixture\n"), "camp-1"); err == nil {
		t.Fatal("missing identity accepted")
	}
	repaired, syntaxBroken, err := InterpretManifestIdentity([]byte(":\n  -"), "camp-1")
	if err != nil || !syntaxBroken || repaired != "camp-1" {
		t.Fatalf("broken manifest = %q broken=%v err=%v", repaired, syntaxBroken, err)
	}

	global := &config.GlobalConfig{
		TUI:    config.TUIConfig{Theme: config.ThemeNameDark},
		Commit: config.CommitPrefs{SyncProjectRefs: true, DisableCommitTags: true},
	}
	local := &config.LocalSettings{Commit: &config.CommitPrefs{}}
	effective, inheritance := localOverlay(global, local)
	eff, _ := effective.(map[string]any)
	commit, _ := eff["commit"].(map[string]any)
	if eff["theme_override"] != config.ThemeNameDark || commit["sync_project_refs"] != false || commit["disable_commit_tags"] != false {
		t.Fatalf("paired commit override lost: %#v", effective)
	}
	inh, _ := inheritance.(map[string]any)
	inhCommit, _ := inh["commit"].(map[string]any)
	if inhCommit["replaced"] != true {
		t.Fatalf("inheritance = %#v", inheritance)
	}
	if looksBinary([]byte("note")) {
		t.Fatal("utf-8 text classified as binary")
	}
	if !looksBinary([]byte{0xff}) {
		t.Fatal("invalid byte 0xff classified as text")
	}
	if !looksBinary([]byte{'a', 0}) {
		t.Fatal("NUL classified as text")
	}
	partial := append([]byte("note"), 0xE2)
	if looksBinary(partial) {
		t.Fatal("incomplete trailing rune classified as binary")
	}
}

func mustEntry(t *testing.T, rel string) DocumentEntry {
	t.Helper()
	entry, err := Classify(rel, DocumentState{Kind: KindFile})
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func assertYAMLScalar(t *testing.T, source []byte, key, tag, value string) {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(source, &doc); err != nil {
		t.Fatal(err)
	}
	root := &doc
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		t.Fatalf("root kind %v\n%s", root.Kind, source)
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		got := root.Content[i+1]
		if got.Tag != tag || got.Value != value {
			t.Fatalf("%s = tag %s value %q, want %s %q", key, got.Tag, got.Value, tag, value)
		}
		return
	}
	t.Fatalf("missing key %s in\n%s", key, source)
}

func fieldNamed(t *testing.T, fields []Field, name string) Field {
	t.Helper()
	for _, field := range fields {
		if len(field.Path) == 1 && field.Path[0] == name {
			return field
		}
	}
	t.Fatalf("missing field %s", name)
	return Field{}
}

func actionSupported(actions []OwnerAction, id string) bool {
	action := actionByID(actions, id)
	return action.Supported
}

func actionByID(actions []OwnerAction, id string) OwnerAction {
	for _, action := range actions {
		if action.ID == id {
			return action
		}
	}
	return OwnerAction{}
}
