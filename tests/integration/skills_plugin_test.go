//go:build integration
// +build integration

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Provider fixtures captured from camp-app (see internal/skills/testdata).
const providerFixtures = "../../internal/skills/testdata/provider/"

const pluginSkillFile = ".campaign/skills/festival-app/SKILL.md"

type updateJSON struct {
	SchemaVersion string `json:"schema_version"`
	DryRun        bool   `json:"dry_run,omitempty"`
	Bundles       []struct {
		Slug                    string `json:"slug"`
		Source                  string `json:"source"`
		ProviderVersion         string `json:"provider_version,omitempty"`
		PreviousProviderVersion string `json:"previous_provider_version,omitempty"`
		Action                  string `json:"action"`
		State                   string `json:"state"`
		Undo                    string `json:"undo,omitempty"`
	} `json:"bundles"`
	Links []struct {
		Tool   string `json:"tool"`
		Path   string `json:"path"`
		Result string `json:"result"`
	} `json:"links"`
	Providers []struct {
		Plugin  string `json:"plugin"`
		Status  string `json:"status"`
		Version string `json:"version,omitempty"`
		Detail  string `json:"detail,omitempty"`
	} `json:"providers"`
}

type declineJSON struct {
	SchemaVersion string   `json:"schema_version"`
	Slug          string   `json:"slug"`
	Source        string   `json:"source"`
	Removed       bool     `json:"removed"`
	LinksRemoved  []string `json:"links_removed"`
	Undo          string   `json:"undo"`
	Restore       string   `json:"restore,omitempty"`
}

func strictJSON(t *testing.T, raw string, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(v), "stdout is not the strict JSON shape:\n%s", raw)
	require.False(t, dec.More(), "stdout holds more than one JSON value:\n%s", raw)
}

// fakeCampApp installs a camp-app script in its own directory, never on the
// shared container's PATH. Each call appends a line to dir/calls, then the
// script body runs.
func fakeCampApp(t *testing.T, tc *TestContainer, name, body string) string {
	t.Helper()
	dir := "/fakes/" + name
	script := "#!/bin/sh\necho \"$*\" >> " + dir + "/calls\n" + body + "\n"
	require.NoError(t, tc.WriteFile(dir+"/camp-app", script))
	tc.Shell(t, "chmod 755 "+dir+"/camp-app && touch "+dir+"/calls")
	return dir
}

// fakeServing is a camp-app that prints answer and exits with exit.
func fakeServing(t *testing.T, tc *TestContainer, name, answer string, exit int) string {
	t.Helper()
	dir := "/fakes/" + name
	require.NoError(t, tc.WriteFile(dir+"/answer.json", answer))
	return fakeCampApp(t, tc, name, "cat "+dir+"/answer.json\nexit "+strconv.Itoa(exit))
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(providerFixtures + name)
	require.NoError(t, err)
	return string(data)
}

// contractWith returns the real camp-app answer with SKILL.md replaced.
func contractWith(t *testing.T, version, content string) string {
	t.Helper()
	var env map[string]any
	require.NoError(t, json.Unmarshal([]byte(fixture(t, "camp-app-contract.json")), &env))
	data := env["data"].(map[string]any)
	data["version"] = version
	file := data["bundles"].([]any)[0].(map[string]any)["files"].([]any)[0].(map[string]any)
	file["content"] = content
	file["sha256"] = sha256Hex(content)
	out, err := json.Marshal(env)
	require.NoError(t, err)
	return string(out)
}

func calls(t *testing.T, tc *TestContainer, fake string) int {
	t.Helper()
	out := tc.Shell(t, "wc -l < "+fake+"/calls")
	n, err := strconv.Atoi(strings.TrimSpace(out))
	require.NoError(t, err)
	return n
}

// campWith runs camp in dir with fake's directory first on PATH, or with no
// camp-app at all when fake is empty, and returns stdout, stderr and the exit
// code.
func campWith(t *testing.T, tc *TestContainer, dir, fake string, args ...string) (string, string, int) {
	t.Helper()
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	tmp := "/tmp/plugin-" + strings.ReplaceAll(dir, "/", "_")
	path := "$PATH"
	if fake != "" {
		path = fake + ":$PATH"
	}
	script := "cd " + dir + " && PATH=" + path + " CAMP_NO_DEFER=1 /camp " + strings.Join(quoted, " ") +
		" >" + tmp + ".out 2>" + tmp + ".err; echo $? >" + tmp + ".code"
	_, _, err := tc.ExecCommand("sh", "-c", script)
	require.NoError(t, err)
	stdout := tc.Shell(t, "cat "+tmp+".out")
	stderr := tc.Shell(t, "cat "+tmp+".err")
	code, err := strconv.Atoi(strings.TrimSpace(tc.Shell(t, "cat "+tmp+".code")))
	require.NoError(t, err)
	return stdout, stderr, code
}

func TestSkillsPlugin_AutoLinkJSONAddsLinksAndRecords(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-auto-link")
	fake := fakeServing(t, tc, "auto-link", fixture(t, "camp-app-contract.json"), 0)

	stdout, stderr, code := campWith(t, tc, path, fake, "skills", "update", "festival-app", "--auto", "--link", "--json")
	t.Logf("camp skills update festival-app --auto --link --json:\n%s", stdout)
	require.Equal(t, 0, code, stderr)
	assert.Empty(t, stderr)
	var got updateJSON
	strictJSON(t, stdout, &got)
	want := `{
  "schema_version": "skills-update/v1alpha1",
  "bundles": [
    {
      "slug": "festival-app",
      "source": "plugin:camp-app",
      "provider_version": "0.3.0-dirty",
      "action": "added",
      "state": "current",
      "undo": "camp skills decline festival-app"
    }
  ],
  "links": [
    {
      "tool": "agents",
      "path": ".agents/skills/festival-app",
      "result": "linked"
    },
    {
      "tool": "claude",
      "path": ".claude/skills/festival-app",
      "result": "linked"
    }
  ],
  "providers": [
    {
      "plugin": "camp-app",
      "status": "ok",
      "version": "0.3.0-dirty"
    }
  ]
}
`
	assert.Equal(t, want, stdout)
	assert.Equal(t, 1, calls(t, tc, fake))

	written := sha256In(t, tc, path+"/"+pluginSkillFile)
	for _, rel := range []string{".claude/skills", ".agents/skills"} {
		assert.Equal(t, written, sha256In(t, tc, path+"/"+rel+"/festival-app/SKILL.md"), "%s reads back other bytes", rel)
	}
	manifest, err := tc.ReadFile(path + skillsManifest)
	require.NoError(t, err)
	assert.Contains(t, manifest, "providers:\n    festival-app:\n        plugin: camp-app\n        version: 0.3.0-dirty\n")

	before := sha256In(t, tc, path+skillsManifest)
	stdout, _, code = campWith(t, tc, path, fake, "skills", "update", "festival-app", "--auto", "--link", "--json")
	require.Equal(t, 0, code)
	strictJSON(t, stdout, &got)
	assert.Equal(t, "none", got.Bundles[0].Action)
	assert.Equal(t, "current", got.Bundles[0].State)
	assert.Equal(t, "already-linked", got.Links[0].Result)
	assert.Equal(t, before, sha256In(t, tc, path+skillsManifest), "a second automatic request rewrote the manifest")
}

// An update from a newer camp-app names both versions, for the app's
// "Updated" notice. An add and a no-op carry no previous version.
func TestSkillsPlugin_UpdateJSONNamesThePreviousVersion(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-previous-version")
	fake := fakeServing(t, tc, "previous-version", fixture(t, "camp-app-contract.json"), 0)
	stdout, stderr, code := campWith(t, tc, path, fake, "skills", "update", "festival-app", "--auto", "--json")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, `"action": "added"`)
	assert.NotContains(t, stdout, "previous_provider_version", "an add has no previous version")

	newer := fakeServing(t, tc, "previous-version-newer",
		contractWith(t, "0.4.0", "---\nname: festival-app\ndescription: newer text\n---\n"), 0)
	stdout, stderr, code = campWith(t, tc, path, newer, "skills", "update", "festival-app", "--auto", "--json")
	t.Logf("update from a newer camp-app:\n%s", stdout)
	require.Equal(t, 0, code, stderr)
	var got updateJSON
	strictJSON(t, stdout, &got)
	assert.Equal(t, `{
  "schema_version": "skills-update/v1alpha1",
  "bundles": [
    {
      "slug": "festival-app",
      "source": "plugin:camp-app",
      "provider_version": "0.4.0",
      "previous_provider_version": "0.3.0-dirty",
      "action": "updated",
      "state": "current"
    }
  ],
  "links": [],
  "providers": [
    {
      "plugin": "camp-app",
      "status": "ok",
      "version": "0.4.0"
    }
  ]
}
`, stdout)

	stdout, stderr, code = campWith(t, tc, path, newer, "skills", "update", "festival-app", "--auto", "--json")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, `"action": "none"`)
	assert.NotContains(t, stdout, "previous_provider_version", "a no-op has no previous version")
}

// A named plugin bundle that does not resolve must never fall through to a
// refresh of camp's own bundles, with or without --auto.
func TestSkillsPlugin_UnresolvedNameNeverRefreshesCampBundles(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-fallthrough")
	makeOutdated(t, tc, path, "campaign-commit")
	commitSkillsFixture(t, tc, path)
	stale := sha256In(t, tc, skillFile(path, "campaign-commit"))
	manifest := sha256In(t, tc, path+skillsManifest)
	_, _, noneOnPath := tc.ExecCommand("sh", "-c", "command -v camp-app")
	require.NotEqual(t, 0, noneOnPath, "the shared container must not have camp-app on PATH")

	providers := map[string]struct {
		fake   string
		status string
		reason string
	}{
		"none":   {"", "not-installed", ""},
		"0.2.1":  {fakeServing(t, tc, "ft-021", fixture(t, "camp-app-0.2.1.json"), 2), "no-contract", "does not offer skills to camp"},
		"0.3.0":  {fakeServing(t, tc, "ft-030", fixture(t, "camp-app-0.3.0.json"), 0), "no-contract", "does not offer skills to camp"},
		"broken": {fakeServing(t, tc, "ft-broken", "panic: boom\n", 2), "invalid", "skipped skills from camp-app"},
	}
	for name, p := range providers {
		stdout, stderr, code := campWith(t, tc, path, p.fake, "skills", "update", "festival-app", "--auto", "--json")
		require.Equal(t, 0, code, "%s --auto: %s", name, stderr)
		var got updateJSON
		strictJSON(t, stdout, &got)
		assert.Empty(t, got.Bundles, "%s --auto", name)
		require.Len(t, got.Providers, 1)
		assert.Equal(t, p.status, got.Providers[0].Status, "%s --auto", name)
		assert.NotEmpty(t, got.Providers[0].Detail, "%s --auto", name)

		stdout, stderr, code = campWith(t, tc, path, p.fake, "skills", "update", "festival-app")
		assert.NotEqual(t, 0, code, "%s: a person's unresolved name must fail", name)
		assert.Empty(t, stdout, "%s", name)
		assert.Contains(t, stderr, "unknown shipped skill bundle festival-app; shipped bundles: ", name)
		if p.reason == "" {
			assert.Regexp(t, `^Error: unknown shipped skill bundle festival-app; shipped bundles: [a-z, -]+\n$`, stderr, "the refusal without camp-app must be today's")
		} else {
			assert.Contains(t, stderr, p.reason, name)
		}

		assert.Equal(t, stale, sha256In(t, tc, skillFile(path, "campaign-commit")), "%s refreshed camp's own bundle", name)
		assert.Equal(t, manifest, sha256In(t, tc, path+skillsManifest), "%s rewrote the manifest", name)
		exists, err := tc.CheckDirExists(path + "/.campaign/skills/festival-app")
		require.NoError(t, err)
		assert.False(t, exists, name)
	}
}

func TestSkillsPlugin_BrokenProviderStillRefreshesANamedCampBundle(t *testing.T) {
	tc := GetSharedContainer(t)
	for name, fake := range map[string]string{
		"garbage": fakeServing(t, tc, "named-garbage", "{\"ok\":true,\n", 0),
		"timeout": fakeCampApp(t, tc, "named-timeout", "sleep 30"),
	} {
		path := initSkillsUpdateCamp(t, tc, "plugin-named-"+name)
		shipped := makeOutdated(t, tc, path, "campaign-commit")
		start := time.Now()
		stdout, stderr, code := campWith(t, tc, path, fake, "skills", "update", "campaign-commit", "festival-app", "--auto")
		elapsed := time.Since(start)
		t.Logf("%s provider, %s:\nstdout:\n%sstderr:\n%s", name, elapsed, stdout, stderr)
		require.Equal(t, 0, code, stderr)
		assert.Contains(t, stderr, "skipped skills from camp-app: ")
		assert.Contains(t, stdout, "updated .campaign/skills/campaign-commit/SKILL.md (outdated)")
		assertNoEmptyLabels(t, stdout+stderr)
		assert.Equal(t, shipped, sha256In(t, tc, skillFile(path, "campaign-commit")))
		assert.Less(t, elapsed, 12*time.Second, "the provider call is not bounded")
	}
}

func TestSkillsPlugin_PlainUpdateAndRepairNeverAskTheProvider(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-plain")
	fake := fakeServing(t, tc, "plain", fixture(t, "camp-app-contract.json"), 0)
	_, stderr, code := campWith(t, tc, path, fake, "skills", "update", "festival-app")
	require.Equal(t, 0, code, stderr)
	tc.Shell(t, "printf '    other-app:\\n        plugin: camp-app\\n        declined: true\\n' >> "+path+skillsManifest)
	plugin := sha256In(t, tc, path+"/"+pluginSkillFile)
	shipped := makeOutdated(t, tc, path, "campaign-commit")
	commitSkillsFixture(t, tc, path)
	asked := calls(t, tc, fake)

	stdout, stderr, code := campWith(t, tc, path, fake, "skills", "update")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "updated .campaign/skills/campaign-commit/SKILL.md (outdated)")
	assert.NotContains(t, stdout+stderr, "festival-app")
	assert.Equal(t, shipped, sha256In(t, tc, skillFile(path, "campaign-commit")))
	commitSkillsFixture(t, tc, path)

	makeOutdated(t, tc, path, "campaign-commit")
	commitSkillsFixture(t, tc, path)
	stdout, stderr, code = campWith(t, tc, path, fake, "init", "--repair", "--yes")
	require.Equal(t, 0, code, stdout+stderr)
	assert.Equal(t, shipped, sha256In(t, tc, skillFile(path, "campaign-commit")))
	dry, stderr, code := campWith(t, tc, path, fake, "init", "--repair", "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.NotContains(t, dry, "record shipped skill hashes", "plugin records alone made the manifest look stale")

	assert.Equal(t, asked, calls(t, tc, fake), "plain update or repair asked camp-app")
	assert.Equal(t, plugin, sha256In(t, tc, path+"/"+pluginSkillFile))
	manifest, err := tc.ReadFile(path + skillsManifest)
	require.NoError(t, err)
	assert.Contains(t, manifest, "    festival-app:\n        plugin: camp-app\n        version: 0.3.0-dirty\n")
	assert.Contains(t, manifest, "    other-app:\n        plugin: camp-app\n        declined: true\n")
}

func TestSkillsPlugin_DeclineJSONThenAutoLeavesItOut(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-decline")
	fake := fakeServing(t, tc, "decline", fixture(t, "camp-app-contract.json"), 0)
	_, stderr, code := campWith(t, tc, path, fake, "skills", "update", "festival-app", "--auto", "--link", "--json")
	require.Equal(t, 0, code, stderr)

	stdout, stderr, code := campWith(t, tc, path, fake, "skills", "decline", "festival-app", "--json")
	t.Logf("camp skills decline festival-app --json:\n%s", stdout)
	require.Equal(t, 0, code, stderr)
	var got declineJSON
	strictJSON(t, stdout, &got)
	assert.Equal(t, `{
  "schema_version": "skills-decline/v1alpha1",
  "slug": "festival-app",
  "source": "plugin:camp-app",
  "removed": true,
  "links_removed": [
    ".agents/skills/festival-app",
    ".claude/skills/festival-app"
  ],
  "undo": "camp skills update festival-app"
}
`, stdout)
	for _, rel := range []string{".campaign/skills/festival-app", ".claude/skills/festival-app", ".agents/skills/festival-app"} {
		_, exit, err := tc.ExecCommand("test", "-e", path+"/"+rel)
		require.NoError(t, err)
		assert.NotEqual(t, 0, exit, "%s still exists", rel)
	}
	manifest, err := tc.ReadFile(path + skillsManifest)
	require.NoError(t, err)
	assert.Contains(t, manifest, "    festival-app:\n        plugin: camp-app\n        version: 0.3.0-dirty\n        declined: true\n")

	stdout, _, code = campWith(t, tc, path, fake, "skills", "update", "festival-app", "--auto", "--link", "--json")
	require.Equal(t, 0, code)
	var auto updateJSON
	strictJSON(t, stdout, &auto)
	require.Len(t, auto.Bundles, 1)
	assert.Equal(t, "none", auto.Bundles[0].Action)
	assert.Equal(t, "declined", auto.Bundles[0].State)
	assert.Empty(t, auto.Links)
	exists, err := tc.CheckDirExists(path + "/.campaign/skills/festival-app")
	require.NoError(t, err)
	assert.False(t, exists, "an automatic request re-added a declined bundle")

	stdout, stderr, code = campWith(t, tc, path, fake, "skills", "update", "festival-app", "--dry-run")
	t.Logf("camp skills update festival-app --dry-run:\n%s", stdout)
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "would add "+pluginSkillFile+" (missing; from camp-app 0.3.0-dirty)")
	assert.NotContains(t, stdout, "undo ", "a dry run has nothing to undo")
	assertNoEmptyLabels(t, stdout)

	stdout, stderr, code = campWith(t, tc, path, fake, "skills", "update", "festival-app")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "added "+pluginSkillFile+" (missing; from camp-app 0.3.0-dirty)")
	assert.Contains(t, stdout, "undo festival-app: camp skills decline festival-app")
	assertNoEmptyLabels(t, stdout)
	manifest, err = tc.ReadFile(path + skillsManifest)
	require.NoError(t, err)
	assert.NotContains(t, manifest, "declined: true")
}

func TestSkillsPlugin_DeclineRefusals(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-decline-refusals")
	fake := fakeServing(t, tc, "decline-refusals", fixture(t, "camp-app-contract.json"), 0)
	_, _, code := campWith(t, tc, path, fake, "skills", "update", "festival-app")
	require.Equal(t, 0, code)

	_, stderr, code := campWith(t, tc, path, fake, "skills", "decline", "campaign-commit", "--json")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, `"code": "validation_error"`)

	tc.Shell(t, "printf 'my rule\\n' >> "+path+"/"+pluginSkillFile)
	edited := sha256In(t, tc, path+"/"+pluginSkillFile)
	_, stderr, code = campWith(t, tc, path, fake, "skills", "decline", "festival-app")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "camp skills decline --force festival-app")
	assert.Equal(t, edited, sha256In(t, tc, path+"/"+pluginSkillFile))

	stdout, stderr, code := campWith(t, tc, path, fake, "skills", "decline", "festival-app", "--force", "--json")
	require.Equal(t, 0, code, stderr)
	var got declineJSON
	strictJSON(t, stdout, &got)
	require.NotEmpty(t, got.Restore)
	tc.Shell(t, "cd "+path+" && "+got.Restore)
	assert.Equal(t, edited, sha256In(t, tc, path+"/"+pluginSkillFile), "the restore command did not bring the edit back")
}

func TestSkillsPlugin_AheadAndAManifestFromAnOlderCamp(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-ahead")
	served := fixture(t, "camp-app-contract.json")
	fake := fakeServing(t, tc, "ahead", served, 0)
	_, _, code := campWith(t, tc, path, fake, "skills", "update", "festival-app")
	require.Equal(t, 0, code)
	written := sha256In(t, tc, path+"/"+pluginSkillFile)

	tc.Shell(t, "sed -i 's/version: 0.3.0-dirty/version: 9.9.9/' "+path+skillsManifest)
	older := fakeServing(t, tc, "ahead-older", contractWith(t, "0.4.0", "---\nname: festival-app\ndescription: older text\n---\n"), 0)
	stdout, stderr, code := campWith(t, tc, path, older, "skills", "update", "festival-app", "--auto", "--json")
	require.Equal(t, 0, code, stderr)
	var got updateJSON
	strictJSON(t, stdout, &got)
	assert.Equal(t, "kept", got.Bundles[0].Action)
	assert.Equal(t, "ahead", got.Bundles[0].State)
	assert.Equal(t, "9.9.9", got.Bundles[0].ProviderVersion)
	assert.Equal(t, written, sha256In(t, tc, path+"/"+pluginSkillFile), "an older camp-app downgraded the bundle")

	// An older camp keeps only the bundles it ships when it rewrites the
	// manifest: no providers and no festival-app record.
	stripOlder := "awk '/^providers:/{exit} /^    festival-app:/{skip=1; next} skip && /^        /{next} {skip=0; print}' " +
		path + skillsManifest + " > /tmp/plugin-ahead.yaml && mv /tmp/plugin-ahead.yaml " + path + skillsManifest
	tc.Shell(t, stripOlder)
	stdout, _, code = campWith(t, tc, path, fake, "skills", "update", "festival-app", "--auto", "--json")
	require.Equal(t, 0, code)
	strictJSON(t, stdout, &got)
	assert.Equal(t, "current", got.Bundles[0].State, "matching bytes with no record must be current")
	manifest, err := tc.ReadFile(path + skillsManifest)
	require.NoError(t, err)
	assert.Contains(t, manifest, "providers:\n    festival-app:\n        plugin: camp-app\n", "the record was not written again")

	tc.Shell(t, stripOlder+" && printf 'edit\\n' >> "+path+"/"+pluginSkillFile)
	edited := sha256In(t, tc, path+"/"+pluginSkillFile)
	stdout, _, code = campWith(t, tc, path, fake, "skills", "update", "festival-app", "--auto", "--json")
	require.Equal(t, 0, code)
	strictJSON(t, stdout, &got)
	assert.Equal(t, "unknown", got.Bundles[0].State)
	assert.Equal(t, "kept", got.Bundles[0].Action)
	assert.Equal(t, edited, sha256In(t, tc, path+"/"+pluginSkillFile))
}

func TestSkillsPlugin_InterruptDuringTheProviderCallWritesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-interrupt")
	fake := fakeCampApp(t, tc, "interrupt", "trap 'echo interrupted >> /fakes/interrupt/calls; exit 130' INT\nsleep 30 &\nwait")
	manifest := sha256In(t, tc, path+skillsManifest)

	script := "cd " + path + " && PATH=" + fake + ":$PATH /camp skills update festival-app --auto --json >/dev/null 2>&1 & pid=$!; " +
		"sleep 1; kill -TERM $pid; start=$(date +%s); wait $pid; echo \"exit=$? secs=$(( $(date +%s) - start ))\""
	out, _, err := tc.ExecCommand("sh", "-c", script)
	require.NoError(t, err)
	t.Logf("interrupted run: %s", out)
	assert.NotContains(t, out, "exit=0", "an interrupted update reported success")
	logged := tc.Shell(t, "cat "+fake+"/calls")
	assert.Contains(t, logged, "interrupted", "camp-app was not interrupted")
	assert.Equal(t, manifest, sha256In(t, tc, path+skillsManifest))
	exists, err := tc.CheckDirExists(path + "/.campaign/skills/festival-app")
	require.NoError(t, err)
	assert.False(t, exists)
}

// assertNoEmptyLabels fails on a human line that prints a label with no value
// after it, such as "undo festival-app: " or "(from camp-app )".
func assertNoEmptyLabels(t *testing.T, out string) {
	t.Helper()
	empty := regexp.MustCompile(`:\s*$|\(\s*\)|\s\)|->\s*[);]|\s{2,}|\s$`)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		assert.False(t, empty.MatchString(line), "line with an empty value: %q", line)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// treeHash fingerprints every path and file under dir except .git, so a test
// can prove a command created, moved or changed nothing.
func treeHash(t *testing.T, tc *TestContainer, dir string) string {
	t.Helper()
	return tc.Shell(t, "cd "+dir+" && find . -path ./.git -prune -o -print | sort | while read -r p; do "+
		"if [ -L \"$p\" ]; then printf '%s -> %s\\n' \"$p\" \"$(readlink \"$p\")\"; "+
		"elif [ -f \"$p\" ]; then printf '%s %s\\n' \"$p\" \"$(sha256sum < \"$p\")\"; else echo \"$p\"; fi; done | sha256sum")
}

func TestSkillsPlugin_NamesThatAreNotBundlesCreateNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-bad-slugs")
	fake := fakeServing(t, tc, "bad-slugs", fixture(t, "camp-app-contract.json"), 0)
	tc.Shell(t, "mkdir -p "+path+"/.campaign/foo /tmp/plugin-abs && printf -- '---\\nname: foo\\ndescription: x\\n---\\n' > "+
		path+"/.campaign/foo/SKILL.md && cp "+path+"/.campaign/foo/SKILL.md /tmp/plugin-abs/SKILL.md")
	before, outside := treeHash(t, tc, path), treeHash(t, tc, "/tmp/plugin-abs")

	for _, slug := range []string{"../foo", "foo/bar", ".hidden", "Foo", "/tmp/plugin-abs"} {
		stdout, stderr, code := campWith(t, tc, path, fake, "skills", "update", "--auto", "--link", slug)
		assert.NotEqual(t, 0, code, "%s was accepted", slug)
		assert.Empty(t, stdout, slug)
		assert.Contains(t, stderr, "unknown shipped skill bundle "+slug+"; shipped bundles: ", slug)
	}
	assert.Equal(t, before, treeHash(t, tc, path), "a bad name changed the camp")
	assert.Equal(t, outside, treeHash(t, tc, "/tmp/plugin-abs"), "a bad name changed a path outside the camp")
	_, linked, err := tc.ExecCommand("test", "-e", path+"/.claude/foo")
	require.NoError(t, err)
	assert.NotEqual(t, 0, linked, ".claude/foo was created")
	assert.Equal(t, 0, calls(t, tc, fake), "camp-app was asked about a name that cannot be a bundle")
}

func TestSkillsPlugin_OddBundleShapeIsKeptAndCampStillRefreshed(t *testing.T) {
	tc := GetSharedContainer(t)
	fake := fakeServing(t, tc, "odd-shape", fixture(t, "camp-app-contract.json"), 0)
	for name, stage := range map[string]string{
		"file":   "printf x > %s/.campaign/skills/festival-app",
		"folder": "mkdir -p %s/.campaign/skills/festival-app/SKILL.md",
	} {
		path := initSkillsUpdateCamp(t, tc, "plugin-odd-"+name)
		shipped := makeOutdated(t, tc, path, "campaign-commit")
		tc.Shell(t, strings.ReplaceAll(stage, "%s", path))

		stdout, stderr, code := campWith(t, tc, path, fake, "skills", "update", "campaign-commit", "festival-app", "--auto", "--json")
		t.Logf("festival-app is a %s:\n%s", name, stdout)
		require.Equal(t, 0, code, stderr)
		var got updateJSON
		strictJSON(t, stdout, &got)
		require.Len(t, got.Bundles, 2)
		assert.Equal(t, "updated", got.Bundles[0].Action)
		assert.Equal(t, "unknown", got.Bundles[1].State)
		assert.Equal(t, "kept", got.Bundles[1].Action)
		assert.Empty(t, got.Bundles[1].Undo)
		assert.Equal(t, shipped, sha256In(t, tc, skillFile(path, "campaign-commit")))

		stdout, _, code = campWith(t, tc, path, fake, "skills", "update", "festival-app")
		require.Equal(t, 0, code)
		assert.Contains(t, stdout, "left festival-app alone: .campaign/skills/festival-app")

		_, stderr, code = campWith(t, tc, path, fake, "skills", "decline", "festival-app", "--force")
		t.Logf("decline when festival-app is a %s: %s", name, stderr)
		assert.NotEqual(t, 0, code)
		want := map[string]string{
			"file":   "decline festival-app: .campaign/skills/festival-app is a file, not a folder",
			"folder": "decline festival-app: .campaign/skills/festival-app/SKILL.md is a folder, not a file",
		}[name]
		assert.Contains(t, stderr, want)
		assert.NotContains(t, stderr, "symlink")
	}
}

func TestSkillsPlugin_DeclinedWithoutCampAppNamesWhatIsMissing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-declined-no-app")
	fake := fakeServing(t, tc, "declined-no-app", fixture(t, "camp-app-contract.json"), 0)
	_, _, code := campWith(t, tc, path, fake, "skills", "update", "festival-app")
	require.Equal(t, 0, code)
	_, _, code = campWith(t, tc, path, fake, "skills", "decline", "festival-app")
	require.Equal(t, 0, code)

	stdout, stderr, code := campWith(t, tc, path, "", "skills", "update", "festival-app")
	t.Logf("declined, no camp-app:\nstdout:\n%sstderr:\n%s", stdout, stderr)
	require.Equal(t, 0, code, stderr)
	assert.NotContains(t, stdout, "run 'camp skills update festival-app'", "the advice repeats the command just run")
	assert.Contains(t, stdout, "left festival-app alone: declined in this camp; adding it back needs camp-app, and camp-app is not installed")
	assertNoEmptyLabels(t, stdout)
}

func TestSkillsPlugin_DryRunLinkJSONCreatesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-dry-link")
	fake := fakeServing(t, tc, "dry-link", fixture(t, "camp-app-contract.json"), 0)
	before := treeHash(t, tc, path)

	stdout, stderr, code := campWith(t, tc, path, fake, "skills", "update", "festival-app", "--link", "--dry-run", "--json")
	t.Logf("camp skills update festival-app --link --dry-run --json:\n%s", stdout)
	require.Equal(t, 0, code, stderr)
	var got updateJSON
	strictJSON(t, stdout, &got)
	assert.Equal(t, `{
  "schema_version": "skills-update/v1alpha1",
  "dry_run": true,
  "bundles": [
    {
      "slug": "festival-app",
      "source": "plugin:camp-app",
      "provider_version": "0.3.0-dirty",
      "action": "added",
      "state": "missing"
    }
  ],
  "links": [
    {
      "tool": "agents",
      "path": ".agents/skills/festival-app",
      "result": "linked"
    },
    {
      "tool": "claude",
      "path": ".claude/skills/festival-app",
      "result": "linked"
    }
  ],
  "providers": [
    {
      "plugin": "camp-app",
      "status": "ok",
      "version": "0.3.0-dirty"
    }
  ]
}
`, stdout)
	assert.Equal(t, before, treeHash(t, tc, path), "a dry run changed the camp")

	stdout, _, code = campWith(t, tc, path, fake, "skills", "update", "festival-app", "--link", "--json")
	require.Equal(t, 0, code)
	var real updateJSON
	strictJSON(t, stdout, &real)
	assert.Equal(t, got.Links, real.Links, "the dry run's links are not the ones the real run made")
	assert.NotContains(t, stdout, "dry_run")
}

func TestSkillsPlugin_StatusNeverAsksCampApp(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-status")
	fake := fakeServing(t, tc, "status", fixture(t, "camp-app-contract.json"), 0)
	_, _, code := campWith(t, tc, path, fake, "skills", "update", "festival-app", "--link")
	require.Equal(t, 0, code)
	asked := calls(t, tc, fake)

	for _, args := range [][]string{{"skills", "status"}, {"skills", "status", "--json"}} {
		with, withErr, withCode := campWith(t, tc, path, fake, args...)
		without, withoutErr, withoutCode := campWith(t, tc, path, "", args...)
		assert.Equal(t, without, with, "%v differs when camp-app is on PATH", args)
		assert.Equal(t, withoutErr, withErr, "%v stderr differs", args)
		assert.Equal(t, withoutCode, withCode, "%v exit differs", args)
	}
	assert.Equal(t, asked, calls(t, tc, fake), "camp skills status asked camp-app")
}

func TestSkillsPlugin_MalformedProvidersNeverBreakCampCommands(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "plugin-bad-providers")
	shipped := makeOutdated(t, tc, path, "campaign-commit")
	tc.Shell(t, "printf 'providers: oops\\n' >> "+path+skillsManifest)
	commitSkillsFixture(t, tc, path)
	for _, args := range [][]string{{"skills", "status"}, {"skills", "status", "--json"}, {"init", "--repair", "--dry-run"}, {"init", "--repair", "--yes"}} {
		stdout, stderr, code := campWith(t, tc, path, "", args...)
		assert.Equal(t, 0, code, "%v: %s%s", args, stdout, stderr)
	}
	assert.Equal(t, shipped, sha256In(t, tc, skillFile(path, "campaign-commit")), "repair did not refresh camp's bundle")

	makeOutdated(t, tc, path, "campaign-commit")
	tc.Shell(t, "printf 'providers: oops\\n' >> "+path+skillsManifest)
	stdout, stderr, code := campWith(t, tc, path, "", "skills", "update")
	assert.Equal(t, 0, code, stdout+stderr)
	assert.Equal(t, shipped, sha256In(t, tc, skillFile(path, "campaign-commit")), "update did not refresh camp's bundle")
}
