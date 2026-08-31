//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Campaign-era compatibility baseline, driven through the real binary against
// real on-disk state.
//
// The host-side half of this baseline lives in internal/compat, where parsing
// and key names are pinned without touching a filesystem. This half covers what
// only the binary can prove: that a workspace created by an older camp is still
// discovered, that a fresh init and a repair leave the frozen paths where they
// are, and that the machine-readable output scripts already parse still carries
// the campaign-shaped keys. Both halves are pinned before the vocabulary change
// so a later failure names the drift instead of describing it.
//
// The frozen list these tests protect is docs/terminology.md, "Frozen technical
// names". A failure here is a contract break, not an expectation to update.

const compatRegistryPath = "/root/.obey/campaign/registry.json"

// oldStateCampaignYAML is the campaign.yaml an older camp wrote: identity,
// type, and nothing else. No concepts block, no workflows taxonomy, no intents
// tags. Discovery and load must not depend on any of the newer sections.
const oldStateCampaignYAML = `id: 8deed8b4-0000-4000-8000-0000000000aa
name: oldstate-camp
type: product
description: A campaign created before the camp vocabulary change
mission: Prove campaign-era metadata still loads unchanged
created_at: 2024-11-04T09:15:00Z
`

// TestCompatOldStateDiscoveryFromCampaignYAMLAlone builds the minimum a
// campaign-era workspace ever had, then reads it from a nested directory. The
// walk-up that finds .campaign/ is the single most load-bearing frozen path in
// the product: if it moves, every existing camp stops being a camp.
func TestCompatOldStateDiscoveryFromCampaignYAMLAlone(t *testing.T) {
	tc := GetSharedContainer(t)
	const root = "/campaigns/oldstate-bare"

	tc.Shell(t, "mkdir -p "+root+"/.campaign "+root+"/projects/deep/nested")
	require.NoError(t, tc.WriteFile(root+"/.campaign/campaign.yaml", oldStateCampaignYAML))

	out, err := tc.RunCampInDir(root+"/projects/deep/nested", "root", "--json")
	require.NoError(t, err, "camp must discover a campaign-era workspace: %s", out)

	var doc struct {
		AbsoluteRoot string `json:"absolute_root"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &doc), "raw=%s", out)
	assert.Equal(t, root, doc.AbsoluteRoot, "walk-up must land on the directory holding .campaign/")

	isDir, err := tc.CheckDirExists(root + "/.camp")
	require.NoError(t, err)
	assert.False(t, isDir, ".camp must stay an attachment marker file, never a workspace directory")
}

// TestCompatOldStateSurfacesCarryCampaignRoot reads the machine-readable
// surfaces from that same bare workspace. campaign_root and the schema versions
// are what a script keys on, and neither is allowed to move for a wording pass.
func TestCompatOldStateSurfacesCarryCampaignRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	const root = "/campaigns/oldstate-surfaces"

	tc.Shell(t, "mkdir -p "+root+"/.campaign")
	require.NoError(t, tc.WriteFile(root+"/.campaign/campaign.yaml", oldStateCampaignYAML))

	tests := []struct {
		name   string
		args   []string
		schema string
	}{
		{name: "concepts", args: []string{"concepts", "--json"}, schema: "concepts/v1alpha1"},
		{name: "workitem", args: []string{"workitem", "--json"}, schema: "workitems/v1alpha12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tc.RunCampInDir(root, tt.args...)
			require.NoError(t, err, "raw=%s", out)

			var doc struct {
				SchemaVersion string `json:"schema_version"`
				CampaignRoot  string `json:"campaign_root"`
			}
			require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &doc), "raw=%s", out)
			assert.Equal(t, tt.schema, doc.SchemaVersion, "a wording change never bumps a schema")
			assert.Equal(t, root, doc.CampaignRoot, "campaign_root is a frozen key")
		})
	}
}

// TestCompatOldStatePreOrgRegistryResolves loads a registry written before orgs,
// tags, and status existed, then resolves a camp out of it by bare name and by
// the org/campaign selector. Scripts and shell aliases pass both forms.
func TestCompatOldStatePreOrgRegistryResolves(t *testing.T) {
	tc := GetSharedContainer(t)
	const root = "/campaigns/oldstate-registry"

	tc.Shell(t, "mkdir -p "+root+"/.campaign")
	require.NoError(t, tc.WriteFile(root+"/.campaign/campaign.yaml", oldStateCampaignYAML))
	require.NoError(t, tc.WriteFile(compatRegistryPath, `{
  "campaigns": {
    "8deed8b4-0000-4000-8000-0000000000aa": {
      "name": "oldstate-camp",
      "path": "`+root+`",
      "type": "product"
    }
  }
}`))

	listed, err := tc.RunCamp("list", "--json")
	require.NoError(t, err, "raw=%s", listed)

	var entries []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Path   string `json:"path"`
		Org    string `json:"org"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal([]byte(listed[strings.Index(listed, "["):]), &entries), "raw=%s", listed)
	require.Len(t, entries, 1, "a pre-org registry entry must not be dropped: %s", listed)
	assert.Equal(t, "oldstate-camp", entries[0].Name)
	assert.Equal(t, "default", entries[0].Org, "a missing org normalizes rather than failing the load")
	assert.Equal(t, "active", entries[0].Status)

	for _, selector := range []string{"oldstate-camp", "default/oldstate-camp"} {
		resolved, err := tc.RunCamp("switch", "--print", selector)
		require.NoError(t, err, "selector %q must still resolve: %s", selector, resolved)
		assert.Contains(t, strings.TrimSpace(resolved), root)
	}
}

// TestCompatOldStateLinkMarkerRecoversCampContext writes a v1 .camp marker, whose
// only binding is the legacy campaign_id field, and reads camp context from
// inside the linked directory. Dropping those legacy fields does not error: a
// directory that worked yesterday simply reports that it is not inside a camp.
func TestCompatOldStateLinkMarkerRecoversCampContext(t *testing.T) {
	tc := GetSharedContainer(t)
	const (
		root     = "/campaigns/oldstate-marker"
		external = "/test/oldstate-linked-repo"
		campID   = "8deed8b4-0000-4000-8000-0000000000aa"
	)

	tc.Shell(t, "mkdir -p "+root+"/.campaign "+root+"/projects")
	require.NoError(t, tc.WriteFile(root+"/.campaign/campaign.yaml", oldStateCampaignYAML))
	require.NoError(t, tc.WriteFile(compatRegistryPath, `{
  "campaigns": {
    "`+campID+`": {
      "name": "oldstate-camp",
      "path": "`+root+`",
      "type": "product"
    }
  }
}`))

	require.NoError(t, tc.CreateGitRepo(external))
	require.NoError(t, tc.WriteFile(external+"/.camp", `{
  "version": 1,
  "campaign_id": "`+campID+`",
  "campaign_root": "`+root+`",
  "project_name": "oldstate-linked-repo"
}`))
	tc.Shell(t, "ln -sfn "+external+" "+root+"/projects/oldstate-linked-repo")

	out, err := tc.RunCampInDir(root+"/projects/oldstate-linked-repo", "root", "--json")
	require.NoError(t, err, "a v1 .camp marker must still recover camp context: %s", out)

	var doc struct {
		AbsoluteRoot string `json:"absolute_root"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &doc), "raw=%s", out)
	assert.Equal(t, root, doc.AbsoluteRoot)
}

// TestCompatInitRepairPreservesCampaignEraLayout runs repair against a workspace
// downgraded to its campaign-era shape. Repair is the command most likely to
// "helpfully" relocate metadata, and relocating it is the specific mistake
// docs/terminology.md says damages a user's camp.
func TestCompatInitRepairPreservesCampaignEraLayout(t *testing.T) {
	tc := GetSharedContainer(t)
	const root = "/campaigns/oldstate-repair"

	_, err := tc.InitCampaign(root, "oldstate-repair", "product")
	require.NoError(t, err)

	before, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	campID := yamlField(t, before, "id")
	require.NotEmpty(t, campID)

	tc.Shell(t, "rm -f "+root+"/.campaign/settings/jumps.yaml "+root+"/AGENTS.md")

	out, err := tc.RunCampInDir(root, "init", "--repair", "--yes", "--no-skills", "--no-register", "-m", "test mission")
	require.NoError(t, err, "repair against campaign-era state must succeed: %s", out)

	after, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err, "repair must not move .campaign/campaign.yaml")
	assert.Equal(t, campID, yamlField(t, after, "id"), "repair must not rewrite the camp id")

	assertFrozenLayout(t, tc, root)
}

// TestCompatFreshInitWritesFrozenLocations pins where a brand new camp puts its
// state. A fresh install and an existing install have to agree on these paths,
// or a user who upgrades finds an empty registry.
func TestCompatFreshInitWritesFrozenLocations(t *testing.T) {
	tc := GetSharedContainer(t)
	const root = "/campaigns/oldstate-fresh"

	_, err := tc.InitCampaign(root, "oldstate-fresh", "product")
	require.NoError(t, err)

	assertFrozenLayout(t, tc, root)

	raw, err := tc.ReadFile(compatRegistryPath)
	require.NoError(t, err, "camp init must write ~/.obey/campaign/registry.json")

	var file map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &file), "raw=%s", raw)
	require.Contains(t, file, "campaigns", "the registry's top-level map is keyed campaigns")
	require.Contains(t, file, "version")

	var camps map[string]struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(file["campaigns"], &camps))
	found := false
	for _, c := range camps {
		if c.Path == root {
			found = true
			assert.Equal(t, "oldstate-fresh", c.Name)
			assert.Equal(t, "product", c.Type)
		}
	}
	assert.True(t, found, "the new camp must be registered under its path: %s", raw)
}

// TestCompatFreshInitWritesFrozenCampaignSubtree pins the paths beneath
// .campaign/ that a fresh init creates. docs/terminology.md freezes the whole
// subtree, and asking the binary what it wrote is the only way to check the
// ones no exported constant names.
func TestCompatFreshInitWritesFrozenCampaignSubtree(t *testing.T) {
	tc := GetSharedContainer(t)
	const root = "/campaigns/oldstate-subtree"

	_, err := tc.InitCampaign(root, "oldstate-subtree", "product")
	require.NoError(t, err)

	listing := tc.Shell(t, "cd "+root+" && find .campaign -maxdepth 2 | sort")

	for _, want := range []string{
		".campaign/campaign.yaml",
		".campaign/watchers.yaml",
		".campaign/intents",
		".campaign/intents/inbox",
		".campaign/intents/active",
		".campaign/intents/ready",
		".campaign/settings",
		".campaign/settings/jumps.yaml",
		".campaign/settings/allowlist.json",
		".campaign/skills",
		".campaign/skills/campaign-commit",
		".campaign/skills/campaign-structure",
		".campaign/skills/campaign-workflows",
		".campaign/skills/cross-campaign",
	} {
		assert.Contains(t, listing, want+"\n", "a fresh camp no longer writes %s:\n%s", want, listing)
	}
}

// TestCompatCampaignFlagTargetsAnotherCamp exercises both spellings of the frozen
// target-camp flag end to end, from inside a different camp. Unit coverage can
// only prove the flag is registered; this proves it still routes.
func TestCompatCampaignFlagTargetsAnotherCamp(t *testing.T) {
	tc := GetSharedContainer(t)
	const (
		here  = "/campaigns/oldstate-flag-here"
		there = "/campaigns/oldstate-flag-there"
	)

	_, err := tc.InitCampaign(here, "oldstate-flag-here", "product")
	require.NoError(t, err)
	_, err = tc.InitCampaign(there, "oldstate-flag-there", "product")
	require.NoError(t, err)

	tests := []struct {
		name string
		flag string
	}{
		{name: "long form", flag: "--campaign"},
		{name: "shorthand", flag: "-c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tc.RunCampInDir(here, "idea", "add", tt.flag, "oldstate-flag-there",
				"routed via "+tt.flag, "--body", "compat baseline", "--json")
			require.NoError(t, err, "%s must still target another camp: %s", tt.flag, out)

			var doc struct {
				Path string `json:"path"`
			}
			require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &doc), "raw=%s", out)
			require.NotEmpty(t, doc.Path, "raw=%s", out)

			// The emitted path is camp-relative, so resolving it against each
			// root is what distinguishes "routed" from "written where I stood".
			landed, err := tc.CheckFileExists(there + "/" + doc.Path)
			require.NoError(t, err)
			assert.True(t, landed, "%s did not write %q into the named camp %s", tt.flag, doc.Path, there)

			stayed, err := tc.CheckFileExists(here + "/" + doc.Path)
			require.NoError(t, err)
			assert.False(t, stayed, "%s wrote %q into the current camp instead of the named one", tt.flag, doc.Path)
		})
	}
}

// TestCompatSwitchJSONKeepsCampaignObject pins the switch contract, which the shell
// integration and the Festival app both read.
func TestCompatSwitchJSONKeepsCampaignObject(t *testing.T) {
	tc := GetSharedContainer(t)
	const root = "/campaigns/oldstate-switch"

	_, err := tc.InitCampaign(root, "oldstate-switch", "product")
	require.NoError(t, err)

	out, err := tc.RunCamp("switch", "--json", "oldstate-switch")
	require.NoError(t, err, "raw=%s", out)

	var doc struct {
		SchemaVersion string `json:"schema_version"`
		Campaign      struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Org    string `json:"org"`
			Status string `json:"status"`
			Path   string `json:"path"`
		} `json:"campaign"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &doc), "raw=%s", out)

	assert.Equal(t, "camp-switch/v1", doc.SchemaVersion)
	assert.Equal(t, "oldstate-switch", doc.Campaign.Name, "the payload's object is keyed campaign")
	assert.Equal(t, root, doc.Campaign.Path)
	assert.NotEmpty(t, doc.Campaign.ID)
	assert.Equal(t, "default", doc.Campaign.Org)
	assert.Equal(t, "active", doc.Campaign.Status)
}

// TestCompatSettingsJSONKeepsCampaignKeys pins in_campaign and campaigns_dir, the two
// campaign-shaped keys in the settings contract.
func TestCompatSettingsJSONKeepsCampaignKeys(t *testing.T) {
	tc := GetSharedContainer(t)
	const root = "/campaigns/oldstate-settings"

	_, err := tc.InitCampaign(root, "oldstate-settings", "product")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(root, "settings", "get", "--json")
	require.NoError(t, err, "raw=%s", out)

	var doc struct {
		SchemaVersion string `json:"schema_version"`
		InCampaign    *bool  `json:"in_campaign"`
		Global        struct {
			CampaignsDir *string `json:"campaigns_dir"`
		} `json:"global"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &doc), "raw=%s", out)

	assert.Equal(t, "settings/v1alpha1", doc.SchemaVersion)
	require.NotNil(t, doc.InCampaign, "in_campaign is a frozen key")
	assert.True(t, *doc.InCampaign)
	require.NotNil(t, doc.Global.CampaignsDir, "campaigns_dir is a frozen key")
}

// assertFrozenLayout checks the three filesystem facts every camp depends on:
// metadata at .campaign/campaign.yaml, user state under ~/.obey/campaign/, and
// .camp never becoming a directory at the camp root.
func assertFrozenLayout(t *testing.T, tc *TestContainer, root string) {
	t.Helper()

	exists, err := tc.CheckFileExists(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.True(t, exists, "camp metadata must stay at .campaign/campaign.yaml")

	exists, err = tc.CheckFileExists(compatRegistryPath)
	require.NoError(t, err)
	assert.True(t, exists, "the registry must stay at ~/.obey/campaign/registry.json")

	isDir, err := tc.CheckDirExists(root + "/.camp")
	require.NoError(t, err)
	assert.False(t, isDir, ".camp is an attachment marker file, never a metadata directory")

	for _, moved := range []string{
		root + "/.camp/camp.yaml",
		root + "/.campaign/camp.yaml",
		"/root/.obey/camp/registry.json",
	} {
		exists, err := tc.CheckFileExists(moved)
		require.NoError(t, err)
		assert.False(t, exists, "%s exists; a frozen path was renamed", moved)
	}
}

// yamlField pulls one top-level scalar out of a small YAML document. The
// fixtures here are flat, so a full parser would only add a dependency.
func yamlField(t *testing.T, doc, key string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if rest, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
