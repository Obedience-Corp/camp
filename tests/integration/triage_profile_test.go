//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTriageInit_ScaffoldsTheDirectory covers first use.
func TestTriageInit_ScaffoldsTheDirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-profile-init", 2, 0)

	output, err := tc.RunCampInDir(path, "triage", "init")
	require.NoError(t, err, output)

	for _, name := range []string{
		"OBEY.md", "profile.yaml",
		"types/_default.yaml", "types/design.yaml", "types/explore.yaml",
		"types/festival.yaml", "types/intent.yaml",
	} {
		body, readErr := tc.ReadFile(path + "/.campaign/triage/" + name)
		require.NoError(t, readErr, "%s must be scaffolded", name)
		assert.NotEmpty(t, body)
	}

	// The profile is commented, not an empty file inheriting invisible
	// defaults — that is the whole reason it is scaffolded.
	profile, err := tc.ReadFile(path + "/.campaign/triage/profile.yaml")
	require.NoError(t, err)
	assert.Contains(t, profile, "# Triage profile")
	assert.Contains(t, profile, "ALWAYS require")

	// Re-running changes nothing.
	output, err = tc.RunCampInDir(path, "triage", "init")
	require.NoError(t, err, output)
	assert.Contains(t, output, "already scaffolded")
}

// TestTriageInit_NeverOverwritesAnEditedProfile is the rule that matters most.
func TestTriageInit_NeverOverwritesAnEditedProfile(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-profile-edited", 2, 0)

	output, err := tc.RunCampInDir(path, "triage", "init")
	require.NoError(t, err, output)

	edited := "# mine\nschema_version: triage-profile/v1alpha1\nreview:\n  batch_size: 9\n"
	require.NoError(t, tc.WriteFile(path+"/.campaign/triage/profile.yaml", edited))

	output, err = tc.RunCampInDir(path, "triage", "init")
	require.NoError(t, err, output)
	assert.Contains(t, output, "differ from the shipped version")
	assert.Contains(t, output, "profile.yaml")

	got, err := tc.ReadFile(path + "/.campaign/triage/profile.yaml")
	require.NoError(t, err)
	assert.Equal(t, edited, got, "the user's edit survives untouched")

	// And the edit is what actually takes effect.
	output, err = tc.RunCampInDir(path, "triage", "profile", "--resolved", "--json")
	require.NoError(t, err, output)

	var payload struct {
		FromFile bool `json:"from_file"`
		Resolved struct {
			Review struct {
				BatchSize int `json:"batch_size"`
			} `json:"review"`
		} `json:"resolved"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &payload))
	assert.True(t, payload.FromFile)
	assert.Equal(t, 9, payload.Resolved.Review.BatchSize)
}

// TestTriageStart_ScaffoldsOnFirstUse: a first run leaves the campaign with a
// readable profile explaining what just happened.
func TestTriageStart_ScaffoldsOnFirstUse(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-profile-start", 2, 0)

	output, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, output)

	guide, err := tc.ReadFile(path + "/.campaign/triage/OBEY.md")
	require.NoError(t, err, "start scaffolds the guide")
	assert.Contains(t, guide, "What no profile can change")
}

// TestTriageProfile_NamedBuiltins covers `--profile sweep|deep`.
func TestTriageProfile_NamedBuiltins(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-profile-named", 2, 0)

	tests := []struct {
		name      string
		wantBatch int
		wantDepth string
	}{
		{name: "sweep", wantBatch: 20, wantDepth: "metadata"},
		{name: "deep", wantBatch: 3, wantDepth: "deep"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tc.RunCampInDir(path, "triage", "profile", "--profile", tt.name, "--json")
			require.NoError(t, err, output)

			var payload struct {
				Name     string `json:"name"`
				FromFile bool   `json:"from_file"`
				Resolved struct {
					Review struct {
						BatchSize int `json:"batch_size"`
					} `json:"review"`
					Evidence struct {
						DepthByStage map[string]string `json:"depth_by_stage"`
					} `json:"evidence"`
				} `json:"resolved"`
			}
			require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &payload))

			assert.Equal(t, tt.name, payload.Name)
			assert.False(t, payload.FromFile, "a named profile is this run's instruction")
			assert.Equal(t, tt.wantBatch, payload.Resolved.Review.BatchSize)
			assert.Equal(t, tt.wantDepth, payload.Resolved.Evidence.DepthByStage["active"])
		})
	}
}

// TestTriageProfile_ValidationNamesFileKeyAndAllowedValues is acceptance 3
// through the binary: a bad profile fails before any snapshot.
func TestTriageProfile_ValidationNamesFileKeyAndAllowedValues(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-profile-invalid", 2, 0)

	output, err := tc.RunCampInDir(path, "triage", "init")
	require.NoError(t, err, output)

	require.NoError(t, tc.WriteFile(path+"/.campaign/triage/profile.yaml",
		"schema_version: triage-profile/v1alpha1\nreview:\n  group_by: vibes\n"))

	output, err = tc.RunCampInDir(path, "triage", "profile", "--resolved")
	require.Error(t, err, "an invalid profile must not resolve")
	assert.Contains(t, output, "profile.yaml")
	assert.Contains(t, output, "review.group_by")
	assert.Contains(t, output, "attention_stage", "the error lists what would work")

	// And it stops a run before anything is snapshotted.
	output, err = tc.RunCampInDir(path, "triage", "start")
	require.Error(t, err, "a bad profile stops the run before the snapshot")
	assert.Contains(t, output, "review.group_by")
}

// TestTriageProfile_CustomTypeTriagesWithZeroConfiguration is acceptance 12:
// a type nobody configured still triages, under types/_default.yaml.
func TestTriageProfile_CustomTypeTriagesWithZeroConfiguration(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-profile-custom-type", 1, 0)

	// A workitem type camp has never heard of.
	tc.Shell(t, fmt.Sprintf(`set -e
cd %s
d=workflow/research/research-item-1
mkdir -p "$d"
printf 'version: v1alpha8\nkind: workitem\nid: research-item-1\ntype: research\ntitle: A research item\n' > "$d/.workitem"
printf '# A research item\n\nBody.\n' > "$d/README.md"
`, path))
	commitAll(t, tc, path, "seed a custom type")

	output, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, output)

	// It is in the run, with a policy, having configured nothing.
	output, err = tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, output)
	assert.Contains(t, output, "research-item-1",
		"a type camp has never seen still triages")

	// And it can be decided, using the fallback vocabulary.
	output, err = tc.RunCampInDir(path, "triage", "evidence", "set", "research-item-1",
		"--no-evidence", "--json")
	require.NoError(t, err, output)
	output, err = tc.RunCampInDir(path, "triage", "propose", "research-item-1",
		"--disposition", "parked", "--summary", "not now", "--json")
	require.NoError(t, err, output,
		"the fallback vocabulary offers a usable disposition with zero configuration")
}

// TestTriageProfile_ResolvedIsEmbeddedInTheManifest is acceptance 5: the
// object printed is the object the run was judged under.
func TestTriageProfile_ResolvedIsEmbeddedInTheManifest(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-profile-embedded", 2, 0)

	output, err := tc.RunCampInDir(path, "triage", "init")
	require.NoError(t, err, output)
	require.NoError(t, tc.WriteFile(path+"/.campaign/triage/profile.yaml",
		"schema_version: triage-profile/v1alpha1\nreview:\n  batch_size: 7\n"))

	_, runDir := startTriageRun(t, tc, path)

	manifest, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)

	var payload struct {
		Profile struct {
			Name     string `json:"name"`
			Resolved struct {
				Review struct {
					BatchSize int `json:"batch_size"`
				} `json:"review"`
			} `json:"resolved"`
		} `json:"profile"`
	}
	require.NoError(t, json.Unmarshal([]byte(manifest), &payload))
	assert.Equal(t, 7, payload.Profile.Resolved.Review.BatchSize,
		"the manifest embeds the profile the run actually used, not the built-in")
}
