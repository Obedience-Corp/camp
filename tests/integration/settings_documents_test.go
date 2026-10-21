//go:build integration

package integration

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const settingsDocumentSchema = "settings-documents/v1alpha1"

func TestIntegration_SettingsDocuments_SelectsOtherCamp(t *testing.T) {
	tc := GetSharedContainer(t)
	a, b := initDocumentCamps(t, tc, "settings-docs-select")
	idB := documentCampID(t, tc, b)
	beforeA := documentTreeHash(t, tc, a)

	for _, cwd := range []string{a, "/tmp"} {
		stdout, stderr, code := runSettingsDocument(t, tc, cwd, "",
			"settings", "document", "get", "campaign.yaml", "--campaign", "settings-docs-select-b", "--json")
		require.Equal(t, 0, code, "cwd %s stderr=%s", cwd, stderr)
		snap := decodeDocumentSnapshot(t, stdout)
		require.Equal(t, settingsDocumentSchema, snap.SchemaVersion)
		require.Equal(t, idB, snap.CampID)
		require.Equal(t, b, snap.CampRoot)
		require.Equal(t, "campaign.yaml", snap.Path)

		stdout, stderr, code = runSettingsDocument(t, tc, cwd, "",
			"settings", "catalog", "--campaign", "settings-docs-select-b", "--json")
		require.Equal(t, 0, code, "catalog cwd %s stderr=%s", cwd, stderr)
		var catalog struct {
			SchemaVersion string `json:"schema_version"`
			CampID        string `json:"camp_id"`
			CampRoot      string `json:"camp_root"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &catalog), stdout)
		require.Equal(t, settingsDocumentSchema, catalog.SchemaVersion)
		require.Equal(t, idB, catalog.CampID)
		require.Equal(t, b, catalog.CampRoot)
	}
	require.Equal(t, beforeA, documentTreeHash(t, tc, a))
}

func TestIntegration_SettingsDocuments_SameNameUsesExplicitID(t *testing.T) {
	tc := GetSharedContainer(t)
	a, b := initDocumentCamps(t, tc, "settings-docs-same-name")
	idA, idB := documentCampID(t, tc, a), documentCampID(t, tc, b)
	require.NotEqual(t, idA, idB)
	// Seed a duplicate-label registry directly: registration itself prompts to
	// replace name collisions, while existing registries can contain them.
	registryPath := strings.TrimSpace(tc.Shell(t, `printf '%s' "${CAMP_REGISTRY_PATH:-${XDG_CONFIG_HOME:-$HOME/.obey}/campaign/registry.json}"`))
	if tc.Shell(t, `test -n "$XDG_CONFIG_HOME" && printf yes || true`) == "yes" {
		registryPath = strings.TrimSpace(tc.Shell(t, `printf '%s' "${CAMP_REGISTRY_PATH:-$XDG_CONFIG_HOME/obey/campaign/registry.json}"`))
	}
	raw, err := tc.ReadFile(registryPath)
	require.NoError(t, err)
	var registry map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &registry))
	for _, root := range []string{a, b} {
		manifest, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
		require.NoError(t, err)
		var values map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(manifest), &values))
		values["name"] = "Same display name"
		encoded, err := yaml.Marshal(values)
		require.NoError(t, err)
		require.NoError(t, tc.WriteFile(root+"/.campaign/campaign.yaml", string(encoded)))
		registry["campaigns"].(map[string]any)[values["id"].(string)].(map[string]any)["name"] = "Same display name"
	}
	encoded, err := json.Marshal(registry)
	require.NoError(t, err)
	require.NoError(t, tc.WriteFile(registryPath, string(encoded)))
	beforeA := documentTreeHash(t, tc, a)
	stdin := writeDocumentStdin(t, tc, "/tmp/settings-docs-id.txt", "explicit ID selected B\n")
	stdout, stderr, code := runSettingsDocument(t, tc, a, stdin,
		"settings", "document", "create", "notes/by-id.txt", "--campaign", idB,
		"--expected-revision", "absent", "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	written := decodeDocumentSnapshot(t, stdout)
	require.True(t, written.Applied)
	require.Equal(t, idB, written.CampID)
	require.Equal(t, b, written.CampRoot)
	reopened := documentGet(t, tc, "/tmp", idB, "notes/by-id.txt")
	require.NotNil(t, reopened.Content)
	require.Equal(t, "explicit ID selected B\n", *reopened.Content)
	require.Equal(t, written.Revision, reopened.Revision)
	require.Equal(t, beforeA, documentTreeHash(t, tc, a))
	require.Equal(t, idA, documentGet(t, tc, b, idA, "campaign.yaml").CampID)
}

func TestIntegration_SettingsDocuments_FullConfigurationReopensWithoutExecuting(t *testing.T) {
	tc := GetSharedContainer(t)
	a, b := initDocumentCamps(t, tc, "settings-docs-full")
	beforeA := documentTreeHash(t, tc, a)
	const sentinel = "/tmp/settings-docs-command-must-not-run"
	command := "touch " + sentinel
	manifest, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	type edit struct {
		path  []string
		value any
	}
	concepts := []any{map[string]any{"name": "workflow", "children": []any{map[string]any{"name": "design", "path": "workflow/design", "children": []any{map[string]any{"name": "drafts", "path": "workflow/design/drafts"}}}}}}
	steps := []any{map[string]any{"name": "acceptance", "run": command, "dir": "scripts", "continue_on_error": true}}
	cases := []struct {
		path, seed string
		edits      []edit
	}{
		{path: "campaign.yaml", seed: manifest + "\n# preserve acceptance comment\ncustom_note: untouched\n", edits: []edit{
			{[]string{"mission"}, "Ship complete Camp management"},
			{[]string{"intents", "tags"}, []any{"delivery", "camp"}},
			{[]string{"concepts"}, concepts},
			{[]string{"workflows", "categories", "delivery"}, map[string]any{"label": "Delivery", "description": "Acceptance workflow"}},
			{[]string{"workflows", "category_by_type", "feature"}, "delivery"},
			{[]string{"hooks", "commit_message", "command"}, command},
		}},
		{path: "settings/jumps.yaml", seed: "# preserve acceptance comment\ncustom_note: untouched\npaths:\n  projects: projects/\n", edits: []edit{
			{[]string{"shortcuts", "acceptance", "path"}, "docs/"},
			{[]string{"shortcuts", "acceptance-command", "command"}, command},
			{[]string{"shortcuts", "acceptance-command", "workdir"}, "workflow/design"},
		}},
		{path: "settings/fresh.yaml", seed: "# preserve acceptance comment\ncustom_note: untouched\ncompleted_runs: off\n", edits: []edit{
			{[]string{"projects", "demo", "branch"}, "release"},
			{[]string{"projects", "demo", "push_upstream"}, false},
			{[]string{"projects", "demo", "follow_up"}, steps},
			{[]string{"follow_up"}, steps},
		}},
		{path: "settings/allowlist.json", seed: `{"version":1,"inherit_defaults":true,"commands":{"kept":{"allowed":false}},"custom_note":"untouched"}`, edits: []edit{
			{[]string{"inherit_defaults"}, false},
			{[]string{"commands", "./scripts/acceptance"}, map[string]any{"allowed": true, "description": "Explicit fixture command"}},
		}},
	}
	for _, sample := range cases {
		t.Run(sample.path, func(t *testing.T) {
			require.NoError(t, tc.WriteFile(b+"/.campaign/"+sample.path, sample.seed))
			initial := documentGet(t, tc, a, "settings-docs-full-b", sample.path)
			changes := make([]map[string]any, 0, len(sample.edits))
			for _, change := range sample.edits {
				changes = append(changes, map[string]any{"op": "set", "path": change.path, "value": change.value})
			}
			body, err := json.Marshal(map[string]any{"changes": changes})
			require.NoError(t, err)
			stdin := writeDocumentStdin(t, tc, "/tmp/settings-docs-full.patch", string(body))
			stdout, stderr, code := runSettingsDocument(t, tc, a, stdin, "settings", "document", "patch", sample.path, "--campaign", "settings-docs-full-b", "--expected-revision", initial.Revision, "--stdin", "--json")
			require.Equal(t, 0, code, stderr)
			written := decodeDocumentSnapshot(t, stdout)
			require.True(t, written.Applied)
			// A separate CLI process reopens the selected camp from outside both camps.
			reopened := documentGet(t, tc, "/tmp", "settings-docs-full-b", sample.path)
			require.Equal(t, written.Revision, reopened.Revision)
			require.Equal(t, b, reopened.CampRoot)
			onDisk, err := tc.ReadFile(b + "/.campaign/" + sample.path)
			require.NoError(t, err)
			require.NotNil(t, reopened.Content)
			require.Equal(t, onDisk, *reopened.Content)
			var sourceValues map[string]any
			if strings.HasSuffix(sample.path, ".json") {
				require.NoError(t, json.Unmarshal([]byte(onDisk), &sourceValues))
			} else {
				require.NoError(t, yaml.Unmarshal([]byte(onDisk), &sourceValues))
				require.Contains(t, onDisk, "# preserve acceptance comment")
			}
			require.Equal(t, "untouched", sourceValues["custom_note"])
			for _, change := range sample.edits {
				require.Equal(t, change.value, documentValueAt(t, sourceValues, change.path), "source path %v", change.path)
				require.Equal(t, change.value, documentValueAt(t, reopened.Values, change.path), "CLI values path %v", change.path)
			}
			if sample.path == "settings/allowlist.json" {
				require.Equal(t, false, documentValueAt(t, sourceValues, []string{"commands", "kept", "allowed"}))
			}
			exists, err := tc.CheckFileExists(sentinel)
			require.NoError(t, err)
			require.False(t, exists, "editing configuration executed a stored command")
			require.Equal(t, beforeA, documentTreeHash(t, tc, a), "explicit write changed the cwd camp")
		})
	}
}

func documentValueAt(t *testing.T, value any, path []string) any {
	t.Helper()
	var current any = value
	for _, key := range path {
		object, ok := current.(map[string]any)
		require.True(t, ok, "object at %s in %v: %#v", key, path, current)
		current, ok = object[key]
		require.True(t, ok, "missing %s in %v", key, path)
	}
	return current
}

func TestIntegration_SettingsDocuments_SetPatchPreservesAndStaleRevisionRefuses(t *testing.T) {
	tc := GetSharedContainer(t)
	_, b := initDocumentCamps(t, tc, "settings-docs-patch")
	tc.Shell(t, "rm -f "+shellQuote(b+"/.campaign/settings/jumps.yaml"))

	const jumps = "# keep this comment\npaths:\n  projects: projects/\nextra_unknown: stay\n"
	body := writeDocumentStdin(t, tc, "/tmp/settings-docs-jumps.yaml", jumps)
	stdout, stderr, code := runSettingsDocument(t, tc, b, body,
		"settings", "document", "create", "settings/jumps.yaml",
		"--expected-revision", "absent", "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	created := decodeDocumentSnapshot(t, stdout)
	require.True(t, created.Applied)
	require.NotNil(t, created.Content)
	require.Contains(t, *created.Content, "# keep this comment")
	require.Contains(t, *created.Content, "extra_unknown: stay")

	got := documentGet(t, tc, b, "", "settings/jumps.yaml")
	require.Equal(t, created.Revision, got.Revision)
	onDisk, err := tc.ReadFile(b + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	require.Contains(t, onDisk, "# keep this comment")
	require.Contains(t, onDisk, "extra_unknown: stay")

	patch := writeDocumentStdin(t, tc, "/tmp/settings-docs-jumps.patch",
		`{"changes":[{"op":"set","path":["shortcuts","notes","path"],"value":"docs/"}]}`)
	stdout, stderr, code = runSettingsDocument(t, tc, b, patch,
		"settings", "document", "patch", "settings/jumps.yaml",
		"--expected-revision", got.Revision, "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	patched := decodeDocumentSnapshot(t, stdout)
	require.True(t, patched.Applied)
	require.NotEqual(t, got.Revision, patched.Revision)
	onDisk, err = tc.ReadFile(b + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	require.Contains(t, onDisk, "# keep this comment")
	require.Contains(t, onDisk, "extra_unknown: stay")
	require.Contains(t, onDisk, "docs/")

	stale := writeDocumentStdin(t, tc, "/tmp/settings-docs-jumps-stale.yaml", jumps+"# stale\n")
	before := onDisk
	_, stderr, code = runSettingsDocument(t, tc, b, stale,
		"settings", "document", "set", "settings/jumps.yaml",
		"--expected-revision", "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"--stdin", "--json")
	require.NotEqual(t, 0, code)
	env := decodeDocumentError(t, stderr)
	require.Equal(t, settingsDocumentSchema, env.SchemaVersion)
	require.Equal(t, "revision_conflict", env.Error.Code)
	after, err := tc.ReadFile(b + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestIntegration_SettingsDocuments_CreateRenameDelete(t *testing.T) {
	tc := GetSharedContainer(t)
	_, b := initDocumentCamps(t, tc, "settings-docs-lifecycle")
	const source = "# authored\nkeep unknown\n"
	body := writeDocumentStdin(t, tc, "/tmp/settings-docs-note.md", source)
	stdout, stderr, code := runSettingsDocument(t, tc, b, body,
		"settings", "document", "create", "notes/readme.md",
		"--expected-revision", "absent", "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	created := decodeDocumentSnapshot(t, stdout)
	require.True(t, created.Applied)
	require.Equal(t, "notes/readme.md", created.Path)
	onDisk, err := tc.ReadFile(b + "/.campaign/notes/readme.md")
	require.NoError(t, err)
	require.Equal(t, source, onDisk)

	stdout, stderr, code = runSettingsDocument(t, tc, b, "",
		"settings", "document", "rename", "notes/readme.md", "notes/done.md",
		"--expected-revision", created.Revision, "--json")
	require.Equal(t, 0, code, stderr)
	renamed := decodeDocumentSnapshot(t, stdout)
	require.True(t, renamed.Applied)
	require.Equal(t, "notes/done.md", renamed.Path)
	exists, err := tc.CheckFileExists(b + "/.campaign/notes/readme.md")
	require.NoError(t, err)
	require.False(t, exists)
	moved, err := tc.ReadFile(b + "/.campaign/notes/done.md")
	require.NoError(t, err)
	require.Equal(t, source, moved)

	stdout, stderr, code = runSettingsDocument(t, tc, b, "",
		"settings", "document", "delete", "notes/done.md",
		"--expected-revision", renamed.Revision, "--json")
	require.Equal(t, 0, code, stderr)
	deleted := decodeDocumentSnapshot(t, stdout)
	require.True(t, deleted.Applied)
	require.False(t, deleted.Exists)
	require.Equal(t, "absent", deleted.Revision)
	exists, err = tc.CheckFileExists(b + "/.campaign/notes/done.md")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestIntegration_SettingsDocuments_RepairsMalformedJumpsAndManifest(t *testing.T) {
	tc := GetSharedContainer(t)
	_, b := initDocumentCamps(t, tc, "settings-docs-repair")
	name := "settings-docs-repair-b"
	original, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	id := documentCampID(t, tc, b)
	tc.Shell(t, "rm -f "+shellQuote(b+"/.campaign/settings/jumps.yaml"))

	require.NoError(t, tc.WriteFile(b+"/.campaign/settings/jumps.yaml", ":\n  -\n"))
	beforeJumps, err := tc.ReadFile(b + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	brokenJumps := documentGet(t, tc, "/tmp", name, "settings/jumps.yaml")
	require.NotNil(t, brokenJumps.Content)
	require.Contains(t, *brokenJumps.Content, ":")
	afterGet, err := tc.ReadFile(b + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	require.Equal(t, beforeJumps, afterGet, "get rewrote malformed jumps")

	repaired := writeDocumentStdin(t, tc, "/tmp/settings-docs-jumps-repair.yaml",
		"# repaired\npaths:\n  projects: projects/\nextra_unknown: stay\n")
	stdout, stderr, code := runSettingsDocument(t, tc, "/tmp", repaired,
		"settings", "document", "set", "settings/jumps.yaml",
		"--campaign", name, "--expected-revision", brokenJumps.Revision, "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	require.True(t, decodeDocumentSnapshot(t, stdout).Applied)
	fixed, err := tc.ReadFile(b + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	require.Contains(t, fixed, "# repaired")
	require.Contains(t, fixed, "extra_unknown: stay")

	require.NoError(t, tc.WriteFile(b+"/.campaign/campaign.yaml", ":\n  -\n"))
	beforeManifest, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	hashBefore := documentTreeHash(t, tc, b)
	brokenManifest := documentGet(t, tc, "/tmp", name, "campaign.yaml")
	require.Equal(t, id, brokenManifest.CampID)
	require.NotNil(t, brokenManifest.Content)
	require.Equal(t, hashBefore, documentTreeHash(t, tc, b), "reading a broken manifest scaffolded files")
	unchanged, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	require.Equal(t, beforeManifest, unchanged)

	restore := writeDocumentStdin(t, tc, "/tmp/settings-docs-manifest.yaml", original)
	stdout, stderr, code = runSettingsDocument(t, tc, "/tmp", restore,
		"settings", "document", "set", "campaign.yaml",
		"--campaign", name, "--expected-revision", brokenManifest.Revision, "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	require.True(t, decodeDocumentSnapshot(t, stdout).Applied)
	require.Equal(t, id, documentCampID(t, tc, b))
	restored, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	require.Contains(t, restored, "id: "+id)
}

func TestIntegration_SettingsDocuments_RejectsBadIdentityWithoutScaffolding(t *testing.T) {
	tc := GetSharedContainer(t)
	a, b := initDocumentCamps(t, tc, "settings-docs-identity")
	name := "settings-docs-identity-b"
	original, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	tc.Shell(t, "rm -f "+shellQuote(a+"/.campaign/settings/jumps.yaml")+" "+shellQuote(b+"/.campaign/settings/jumps.yaml"))

	for _, defect := range []string{"missing-id", "mismatched-id"} {
		var manifest map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(original), &manifest))
		if defect == "missing-id" {
			delete(manifest, "id")
		} else {
			manifest["id"] = "00000000-0000-4000-8000-000000000001"
		}
		data, err := yaml.Marshal(manifest)
		require.NoError(t, err)
		require.NoError(t, tc.WriteFile(b+"/.campaign/campaign.yaml", string(data)))
		for _, cwd := range []string{a, "/tmp"} {
			beforeA := documentTreeHash(t, tc, a)
			beforeB := documentTreeHash(t, tc, b)
			_, stderr, code := runSettingsDocument(t, tc, cwd, "",
				"settings", "document", "get", "campaign.yaml", "--campaign", name, "--json")
			require.NotEqual(t, 0, code, "cwd %s defect %s stderr=%s", cwd, defect, stderr)
			env := decodeDocumentError(t, stderr)
			require.Equal(t, settingsDocumentSchema, env.SchemaVersion)
			require.Equal(t, "identity_mismatch", env.Error.Code)
			require.NotContains(t, stderr, "Usage:")
			require.Equal(t, beforeA, documentTreeHash(t, tc, a))
			require.Equal(t, beforeB, documentTreeHash(t, tc, b))
			_, exitCode, err := tc.ExecCommand("test", "!", "-e", b+"/.campaign/settings/jumps.yaml")
			require.NoError(t, err)
			require.Equal(t, 0, exitCode, "identity refusal scaffolded jumps.yaml")
		}
	}
}

func TestIntegration_SettingsDocuments_StdinAndArgsJSONErrors(t *testing.T) {
	tc := GetSharedContainer(t)
	_, b := initDocumentCamps(t, tc, "settings-docs-errors")

	_, stderr, code := runSettingsDocument(t, tc, b, "",
		"settings", "document", "get", "--json")
	require.NotEqual(t, 0, code)
	assertDocumentJSONError(t, stderr, "validation_error")

	_, stderr, code = runSettingsDocument(t, tc, b, "",
		"settings", "document", "rename", "notes/only.md", "--json")
	require.NotEqual(t, 0, code)
	assertDocumentJSONError(t, stderr, "validation_error")

	_, stderr, code = runSettingsDocument(t, tc, b, "",
		"settings", "document", "set", "notes/missing.md",
		"--expected-revision", "absent", "--json")
	require.NotEqual(t, 0, code)
	assertDocumentJSONError(t, stderr, "validation_error")

	_, stderr, code = runSettingsDocument(t, tc, b, "",
		"settings", "document", "create", "notes/missing.md",
		"--expected-revision", "", "--stdin", "--json")
	require.NotEqual(t, 0, code)
	assertDocumentJSONError(t, stderr, "validation_error")

	tc.Shell(t, "dd if=/dev/zero of=/tmp/settings-docs-oversize bs=1048577 count=1 status=none")
	_, stderr, code = runSettingsDocument(t, tc, b, "/tmp/settings-docs-oversize",
		"settings", "document", "validate", "notes/huge.md", "--stdin", "--json")
	require.NotEqual(t, 0, code)
	assertDocumentJSONError(t, stderr, "oversized")
	exists, err := tc.CheckFileExists(b + "/.campaign/notes/huge.md")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestIntegration_SettingsDocuments_OwnerActionsPersist(t *testing.T) {
	tc := GetSharedContainer(t)
	_, b := initDocumentCamps(t, tc, "settings-docs-actions")
	name := "settings-docs-actions-b"
	tc.Shell(t, "mkdir -p "+shellQuote(b+"/notes"))
	tc.Shell(t, "rm -f "+shellQuote(b+"/.campaign/settings/pins.json")+" "+shellQuote(b+"/.campaign/settings/workitems.json"))

	pinsRev := documentGet(t, tc, b, name, "settings/pins.json").Revision
	require.Equal(t, "absent", pinsRev)
	pinBody := writeDocumentStdin(t, tc, "/tmp/settings-docs-pin.json", `{"name":"dock","path":"notes"}`)
	stdout, stderr, code := runSettingsDocument(t, tc, "/tmp", pinBody,
		"settings", "document", "action", "settings/pins.json", "pin",
		"--campaign", name, "--expected-revision", pinsRev, "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	pinned := decodeDocumentSnapshot(t, stdout)
	require.True(t, pinned.Applied)
	pins, err := tc.ReadFile(b + "/.campaign/settings/pins.json")
	require.NoError(t, err)
	require.Contains(t, pins, "dock")
	require.Contains(t, pins, "notes")

	unpinBody := writeDocumentStdin(t, tc, "/tmp/settings-docs-unpin.json", `{"name":"dock"}`)
	stdout, stderr, code = runSettingsDocument(t, tc, b, unpinBody,
		"settings", "document", "action", "settings/pins.json", "unpin",
		"--campaign", name, "--expected-revision", pinned.Revision, "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	require.True(t, decodeDocumentSnapshot(t, stdout).Applied)
	pins, err = tc.ReadFile(b + "/.campaign/settings/pins.json")
	require.NoError(t, err)
	require.NotContains(t, pins, "dock")

	const intentID = "doc-priority-20261021-000001"
	intent := "---\nid: " + intentID + "\ntitle: Document priority\nstatus: active\ncreated_at: 2026-10-21T00:00:00Z\ntype: bug\n---\n\n# Document priority\n"
	require.NoError(t, tc.WriteFile(b+"/.campaign/intents/active/"+intentID+".md", intent))
	workRev := documentGet(t, tc, b, name, "settings/workitems.json").Revision
	priorityBody := writeDocumentStdin(t, tc, "/tmp/settings-docs-priority.json",
		`{"selector":"`+intentID+`","priority":"high"}`)
	stdout, stderr, code = runSettingsDocument(t, tc, b, priorityBody,
		"settings", "document", "action", "settings/workitems.json", "workitem-priority",
		"--campaign", name, "--expected-revision", workRev, "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	prioritized := decodeDocumentSnapshot(t, stdout)
	require.True(t, prioritized.Applied)
	work, err := tc.ReadFile(b + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	require.Contains(t, work, intentID)
	require.Contains(t, work, "high")

	intentGroup := writeDocumentStdin(t, tc, "/tmp/settings-docs-intent-group.json",
		`{"selector":"`+intentID+`","group":"launch"}`)
	_, stderr, code = runSettingsDocument(t, tc, "/tmp", intentGroup,
		"settings", "document", "action", "settings/workitems.json", "workitem-group",
		"--campaign", name, "--expected-revision", prioritized.Revision, "--stdin", "--json")
	require.NotEqual(t, 0, code, "intent group must be refused: %s", stderr)
	unchanged, err := tc.ReadFile(b + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	require.Equal(t, work, unchanged, "refused intent group changed the store")

	out, err := tc.RunCampInDir(b, "workitem", "create", "docs-group", "--type", "design", "--title", "Docs group")
	require.NoError(t, err, out)
	workRev = documentGet(t, tc, b, name, "settings/workitems.json").Revision
	designGroup := writeDocumentStdin(t, tc, "/tmp/settings-docs-group.json",
		`{"selector":"workflow/design/docs-group","group":"launch"}`)
	_, stderr, code = runSettingsDocument(t, tc, "/tmp", designGroup,
		"settings", "document", "action", "settings/workitems.json", "workitem-group",
		"--campaign", name, "--expected-revision", workRev, "--stdin", "--json")
	require.Equal(t, 0, code, stderr)
	work, err = tc.ReadFile(b + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	require.Contains(t, work, "launch")
	require.Contains(t, work, "high")
	require.Contains(t, work, intentID)
}

func initDocumentCamps(t *testing.T, tc *TestContainer, prefix string) (a, b string) {
	t.Helper()
	a = "/test/" + prefix + "-a"
	b = "/test/" + prefix + "-b"
	for _, name := range []string{prefix + "-a", prefix + "-b"} {
		out, err := tc.RunCamp("init", "/test/"+name, "--name", name, "-d", "Document API test", "-m", "original", "--no-git", "--force")
		require.NoError(t, err, out)
	}
	return a, b
}

func documentCampID(t *testing.T, tc *TestContainer, root string) string {
	t.Helper()
	raw, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	var manifest struct {
		ID string `yaml:"id"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(raw), &manifest))
	require.NotEmpty(t, manifest.ID)
	return manifest.ID
}

func documentTreeHash(t *testing.T, tc *TestContainer, root string) string {
	t.Helper()
	return tc.Shell(t, "cd "+shellQuote(root)+" && find .campaign -type f -exec sha256sum {} \\; | sort")
}

func documentGet(t *testing.T, tc *TestContainer, cwd, campName, ref string) documentSnapshot {
	t.Helper()
	args := []string{"settings", "document", "get", ref, "--json"}
	if campName != "" {
		args = append(args, "--campaign", campName)
	}
	stdout, stderr, code := runSettingsDocument(t, tc, cwd, "", args...)
	require.Equal(t, 0, code, stderr)
	return decodeDocumentSnapshot(t, stdout)
}

func writeDocumentStdin(t *testing.T, tc *TestContainer, path, content string) string {
	t.Helper()
	require.NoError(t, tc.WriteFile(path, content))
	return path
}

func runSettingsDocument(t *testing.T, tc *TestContainer, dir, stdinFile string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	redirect := ""
	if stdinFile != "" {
		redirect = " < " + shellQuote(stdinFile)
	}
	script := "cd " + shellQuote(dir) + " && " + tc.campEnvPrefix() + "/camp " + strings.Join(quoted, " ") + redirect +
		" >/tmp/_doc_out 2>/tmp/_doc_err; echo $? >/tmp/_doc_code"
	_, _, err := tc.ExecCommand("sh", "-c", script)
	require.NoError(t, err)
	stdout, _, err = tc.ExecCommand("cat", "/tmp/_doc_out")
	require.NoError(t, err)
	stderr, _, err = tc.ExecCommand("cat", "/tmp/_doc_err")
	require.NoError(t, err)
	codeText, _, err := tc.ExecCommand("cat", "/tmp/_doc_code")
	require.NoError(t, err)
	exitCode, err = strconv.Atoi(strings.TrimSpace(codeText))
	require.NoError(t, err, codeText)
	_, _, _ = tc.ExecCommand("rm", "-f", "/tmp/_doc_out", "/tmp/_doc_err", "/tmp/_doc_code")
	return stdout, stderr, exitCode
}

type documentSnapshot struct {
	SchemaVersion string  `json:"schema_version"`
	CampID        string  `json:"camp_id"`
	CampRoot      string  `json:"camp_root"`
	Path          string  `json:"path"`
	Exists        bool    `json:"exists"`
	Revision      string  `json:"revision"`
	Content       *string `json:"content"`
	Applied       bool    `json:"applied"`
	Values        any     `json:"values"`
}

type documentError struct {
	SchemaVersion string `json:"schema_version"`
	Error         struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeDocumentSnapshot(t *testing.T, stdout string) documentSnapshot {
	t.Helper()
	var snap documentSnapshot
	require.NoError(t, json.Unmarshal([]byte(stdout), &snap), stdout)
	require.Equal(t, settingsDocumentSchema, snap.SchemaVersion)
	return snap
}

func decodeDocumentError(t *testing.T, stderr string) documentError {
	t.Helper()
	var env documentError
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(stderr)), &env), stderr)
	require.NotEmpty(t, env.Error.Code, stderr)
	require.NotEmpty(t, env.Error.Message, stderr)
	return env
}

func assertDocumentJSONError(t *testing.T, stderr, code string) {
	t.Helper()
	require.NotContains(t, stderr, "Usage:")
	env := decodeDocumentError(t, stderr)
	require.Equal(t, settingsDocumentSchema, env.SchemaVersion)
	require.Equal(t, code, env.Error.Code)
}
