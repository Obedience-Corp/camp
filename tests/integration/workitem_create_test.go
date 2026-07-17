//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemCreateAndAdopt(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workitem-create"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Create Test",
		"--type", "product",
		"-d", "Workitem create+adopt integration",
		"-m", "Verify create and adopt subcommands",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	t.Run("CreateRefreshesNavigationCache", func(t *testing.T) {
		_, err := tc.RunCampInDir(campaignDir, "complete", "de")
		require.NoError(t, err, "initial design completion should build nav cache")
		_, _, err = tc.ExecCommand("test", "-f", campaignDir+"/.campaign/cache/nav-index.json")
		require.NoError(t, err, "expected initial completion to create nav cache")

		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "nav-design", "--type", "design", "--title", "Navigation Design")
		require.NoError(t, err, "camp workitem create design: %s", out)
		assert.Contains(t, out, "created workflow/design/nav-design")

		out, err = tc.RunCampInDir(campaignDir, "complete", "de")
		require.NoError(t, err, "completion after workitem create: %s", out)
		assert.Contains(t, out, "nav-design",
			"newly created design workitem must be visible without manual cache rebuild:\n%s", out)

		out, err = tc.RunCampInDir(campaignDir, "go", "de", "nav-design", "--print")
		require.NoError(t, err, "navigation after workitem create: %s", out)
		assert.Contains(t, out, "workflow/design/nav-design")
	})

	t.Run("CreateBuildsDirectoryAndWorkitem", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "demo-feature", "--type", "feature", "--title", "Demo")
		require.NoError(t, err, "camp workitem create: %s", out)
		assert.Contains(t, out, "created workflow/feature/demo-feature")
		assert.Contains(t, out, "id: feature-demo-feature-")
		assert.Contains(t, out, "type: feature")

		manifest, err := tc.ReadFile(campaignDir + "/workflow/feature/demo-feature/.workitem")
		require.NoError(t, err)
		assert.Contains(t, manifest, "version: v1alpha7")
		assert.Contains(t, manifest, "kind: workitem")
		assert.Contains(t, manifest, "type: feature")
		assert.Contains(t, manifest, "title: Demo")
		assert.Regexp(t, `ref: WI-[0-9a-f]{6}`, manifest)
	})

	t.Run("CreateRefusesExistingDirectory", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "demo-feature", "--type", "feature")
		require.Error(t, err, "expected error for existing dir")
		assert.True(t,
			strings.Contains(out, "target directory already exists") || strings.Contains(out, "already exists"),
			"error should mention existing dir, got: %s", out)
	})

	t.Run("CreateRejectsInvalidSlug", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "Bad Slug!")
		require.Error(t, err, "expected error for invalid slug")
		assert.Contains(t, out, "invalid slug")
	})

	t.Run("AdoptAddsMarkerToExistingDir", func(t *testing.T) {
		_, _, err := tc.ExecCommand("mkdir", "-p", campaignDir+"/workflow/incident/p99-spike")
		require.NoError(t, err)
		out, err := tc.RunCampInDir(campaignDir, "workitem", "adopt", "workflow/incident/p99-spike", "--type", "incident", "--title", "P99 spike")
		require.NoError(t, err, "camp workitem adopt: %s", out)
		assert.Contains(t, out, "adopted workflow/incident/p99-spike")

		manifest, err := tc.ReadFile(campaignDir + "/workflow/incident/p99-spike/.workitem")
		require.NoError(t, err)
		assert.Contains(t, manifest, "type: incident")
		assert.Contains(t, manifest, "title: P99 spike")
	})

	t.Run("AdoptRefusesAlreadyAdopted", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "adopt", "workflow/incident/p99-spike", "--type", "incident")
		require.Error(t, err, "expected error for already-adopted dir")
		assert.Contains(t, out, "already")
	})

	t.Run("CreatedAndAdoptedAppearInDashboard", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "--json=true")
		require.NoError(t, err, "camp workitem --json: %s", out)
		assert.Contains(t, out, "workflow/feature/demo-feature",
			"created workitem must appear in camp workitem dashboard:\n%s", out)
		assert.Contains(t, out, "workflow/incident/p99-spike",
			"adopted workitem must appear in camp workitem dashboard:\n%s", out)
		assert.Contains(t, out, `"workflow_type": "feature"`,
			"created workitem should carry its custom workflow_type:\n%s", out)
		assert.Contains(t, out, `"workflow_type": "incident"`,
			"adopted workitem should carry its custom workflow_type:\n%s", out)
	})

	t.Run("UnmarkedCustomDirIsNotDiscovered", func(t *testing.T) {
		_, _, err := tc.ExecCommand("mkdir", "-p", campaignDir+"/workflow/feature/legacy-no-marker")
		require.NoError(t, err)
		out, err := tc.RunCampInDir(campaignDir, "workitem", "--json=true")
		require.NoError(t, err, "camp workitem --json: %s", out)
		assert.NotContains(t, out, "workflow/feature/legacy-no-marker",
			"directory without .workitem marker must not appear in dashboard:\n%s", out)
	})

	t.Run("DungeonedCustomDirIsNotDiscovered", func(t *testing.T) {
		_, _, err := tc.ExecCommand("mkdir", "-p", campaignDir+"/workflow/feature/dungeon")
		require.NoError(t, err)
		_, _, err = tc.ExecCommand("sh", "-c",
			"echo 'version: v1alpha5\nkind: workitem\nid: x\ntype: feature\ntitle: X' > "+
				campaignDir+"/workflow/feature/dungeon/.workitem")
		require.NoError(t, err)
		out, err := tc.RunCampInDir(campaignDir, "workitem", "--json=true")
		require.NoError(t, err, "camp workitem --json: %s", out)
		assert.NotContains(t, out, "workflow/feature/dungeon",
			"dungeoned dir must be skipped even with a marker:\n%s", out)
	})

	t.Run("DashboardFilterAcceptsCustomType", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "--json=true", "--type", "feature")
		require.NoError(t, err, "filter --type=feature must be accepted: %s", out)
		assert.Contains(t, out, "workflow/feature/demo-feature",
			"--type=feature should surface created feature workitem:\n%s", out)
		assert.NotContains(t, out, "workflow/incident/p99-spike",
			"--type=feature should exclude incident workitems:\n%s", out)
	})

	t.Run("CreateRejectsDuplicateExplicitID", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir,
			"workitem", "create", "dup-id-target",
			"--type", "feature",
			"--id", "feature-demo-feature-fixed-1",
		)
		require.NoError(t, err, "first explicit-id create: %s", out)

		out, err = tc.RunCampInDir(campaignDir,
			"workitem", "create", "dup-id-collider",
			"--type", "feature",
			"--id", "feature-demo-feature-fixed-1",
		)
		require.Error(t, err, "expected error for duplicate explicit id")
		assert.Contains(t, out, "collides",
			"duplicate explicit-id should be rejected with collision error, got: %s", out)
	})
}

func TestIntegration_WorkitemCreateJSON(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workitem-create-json"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Create JSON Test",
		"--type", "product",
		"-d", "Workitem create JSON integration",
		"-m", "Verify create --json contract",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	out, err := tc.RunCampInDir(campaignDir,
		"workitem", "create", "agent-json",
		"--type", "feature",
		"--title", "Agent JSON",
		"--id", "agent-json-fixed",
		"--json",
	)
	require.NoError(t, err, "camp workitem create --json: %s", out)
	assert.NotContains(t, out, "created workflow/feature/agent-json")
	assert.NotContains(t, out, "\nnext:")

	var payload struct {
		SchemaVersion string    `json:"schema_version"`
		GeneratedAt   time.Time `json:"generated_at"`
		Workitem      struct {
			ID            string `json:"id"`
			Ref           string `json:"ref"`
			Type          string `json:"type"`
			Title         string `json:"title"`
			QuestID       string `json:"quest_id"`
			RelativePath  string `json:"relative_path"`
			MarkerVersion string `json:"marker_version"`
		} `json:"workitem"`
		Next struct {
			Command string `json:"command"`
			Cwd     string `json:"cwd"`
			Hint    string `json:"hint"`
		} `json:"next"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload), "raw=%s", out)
	assert.Equal(t, "workitem-create/v1alpha1", payload.SchemaVersion)
	assert.False(t, payload.GeneratedAt.IsZero())
	assert.Equal(t, "agent-json-fixed", payload.Workitem.ID)
	assert.Regexp(t, `^WI-[0-9a-f]{6}$`, payload.Workitem.Ref)
	assert.Equal(t, "feature", payload.Workitem.Type)
	assert.Equal(t, "Agent JSON", payload.Workitem.Title)
	assert.Empty(t, payload.Workitem.QuestID)
	assert.Equal(t, "workflow/feature/agent-json", payload.Workitem.RelativePath)
	assert.Equal(t, "v1alpha7", payload.Workitem.MarkerVersion)
	assert.Equal(t, "fest create workflow agent-json", payload.Next.Command)
	assert.Equal(t, "workflow/feature/agent-json", payload.Next.Cwd)
	assert.Contains(t, payload.Next.Hint, "cd workflow/feature/agent-json")

	resolveOut, err := tc.RunCampInDir(campaignDir,
		"workitem", "resolve", "--workitem", payload.Workitem.ID, "--json")
	require.NoError(t, err, "resolve returned workitem: %s", resolveOut)
	assert.Contains(t, resolveOut, payload.Workitem.ID)
}
