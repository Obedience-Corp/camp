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
		assert.Contains(t, out, "Created design workitem nav-design")
		assert.Regexp(t, `path:\s+workflow/design/nav-design\n`, out)
		assert.Contains(t, out, "Tracking only")
		assert.Regexp(t, `next:\s+cd workflow/design/nav-design && fest create workflow nav-design\n`, out)
		assert.NotContains(t, out, "optional next:")

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
		assert.Contains(t, out, "Created feature workitem demo-feature")
		assert.Regexp(t, `path:\s+workflow/feature/demo-feature\n`, out)
		assert.Regexp(t, `id:\s+feature-demo-feature-`, out)
		assert.Regexp(t, `ref:\s+WI-[0-9a-f]{6}\n`, out)
		assert.Regexp(t, `type:\s+feature\n`, out, "explicit --type carries no provenance note")
		assert.Contains(t, out, "Tracking only")
		assert.NotContains(t, out, "optional next:")
		assert.NotContains(t, out, "next:")
		assert.NotContains(t, out, "fest create workflow")
		assert.NotContains(t, out, "—", "human output must not contain an em dash")

		manifest, err := tc.ReadFile(campaignDir + "/workflow/feature/demo-feature/.workitem")
		require.NoError(t, err)
		assert.Contains(t, manifest, "version: v1alpha9")
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
		assert.Contains(t, manifest, "version: v1alpha9")
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
	assert.NotContains(t, out, "Created feature workitem agent-json")
	assert.NotContains(t, out, "\n  optional next:")
	assert.NotContains(t, out, "\n  next:")

	var payload struct {
		SchemaVersion string    `json:"schema_version"`
		GeneratedAt   time.Time `json:"generated_at"`
		Workitem      struct {
			ID            string   `json:"id"`
			Ref           string   `json:"ref"`
			Type          string   `json:"type"`
			Title         string   `json:"title"`
			QuestID       string   `json:"quest_id"`
			RelativePath  string   `json:"relative_path"`
			MarkerVersion string   `json:"marker_version"`
			Tags          []string `json:"tags"`
			Projects      []string `json:"projects"`
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
	assert.Equal(t, "v1alpha9", payload.Workitem.MarkerVersion)
	require.NotNil(t, payload.Workitem.Tags, "tags must serialize as [], never null")
	require.NotNil(t, payload.Workitem.Projects, "projects must serialize as [], never null")
	assert.Empty(t, payload.Workitem.Tags)
	assert.Empty(t, payload.Workitem.Projects)
	// feature/bug/chore: no agent-executable scaffold command
	assert.Empty(t, payload.Next.Command,
		"non-explore/design types must not ship unconditional fest create workflow")
	assert.NotContains(t, out, `"command"`,
		"empty next.command should be omitted via omitempty for non-scaffold types")
	assert.Equal(t, "workflow/feature/agent-json", payload.Next.Cwd)
	assert.Contains(t, payload.Next.Hint, "tracking only",
		"agent-facing hint must lock the metadata-only signal")
	assert.Contains(t, payload.Next.Hint, "workflow/feature/agent-json")
	assert.NotContains(t, payload.Next.Hint, "fest create workflow",
		"feature create must not recommend festival scaffold")

	// explore/design retain recommended next.command + tracking-only hint
	designOut, err := tc.RunCampInDir(campaignDir,
		"workitem", "create", "agent-design",
		"--type", "design",
		"--title", "Agent Design",
		"--id", "agent-design-fixed",
		"--json",
	)
	require.NoError(t, err, "camp workitem create design --json: %s", designOut)
	var designPayload struct {
		Next struct {
			Command string `json:"command"`
			Cwd     string `json:"cwd"`
			Hint    string `json:"hint"`
		} `json:"next"`
		Workitem struct {
			Type string `json:"type"`
		} `json:"workitem"`
	}
	require.NoError(t, json.Unmarshal([]byte(designOut), &designPayload), "raw=%s", designOut)
	assert.Equal(t, "design", designPayload.Workitem.Type)
	assert.Equal(t, "fest create workflow agent-design", designPayload.Next.Command)
	assert.Equal(t, "workflow/design/agent-design", designPayload.Next.Cwd)
	assert.Contains(t, designPayload.Next.Hint, "tracking only",
		"agent-facing hint must lock the metadata-only signal")
	assert.Contains(t, designPayload.Next.Hint, "recommended next")
	assert.Contains(t, designPayload.Next.Hint, "cd workflow/design/agent-design")

	resolveOut, err := tc.RunCampInDir(campaignDir,
		"workitem", "resolve", "--workitem", payload.Workitem.ID, "--json")
	require.NoError(t, err, "resolve returned workitem: %s", resolveOut)
	assert.Contains(t, resolveOut, payload.Workitem.ID)
}

type workitemCreateInferPayload struct {
	Workitem struct {
		Type         string `json:"type"`
		RelativePath string `json:"relative_path"`
	} `json:"workitem"`
	Next struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	} `json:"next"`
}

func decodeWorkitemCreateInfer(t *testing.T, out string) workitemCreateInferPayload {
	t.Helper()
	var payload workitemCreateInferPayload
	require.NoError(t, json.Unmarshal([]byte(out), &payload), "raw=%s", out)
	return payload
}

func TestIntegration_WorkitemCreateInfersTypeFromLocation(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workitem-create-infer"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Create Infer Test",
		"--type", "product",
		"-d", "Workitem create type inference",
		"-m", "Verify create infers the type from where it runs",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	assertMarkerType := func(t *testing.T, rel, wantType string) {
		t.Helper()
		manifest, err := tc.ReadFile(campaignDir + "/" + rel + "/.workitem")
		require.NoError(t, err, "marker missing for %s", rel)
		assert.Contains(t, manifest, "type: "+wantType+"\n")
	}

	t.Run("FromTypeDirectory", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir+"/workflow/explore", "workitem", "create", "agent-chat-eval")
		require.NoError(t, err, "create from workflow/explore: %s", out)
		t.Logf("human output from workflow/explore:\n%s", out)
		assert.Contains(t, out, "Created explore workitem agent-chat-eval")
		assert.Regexp(t, `path:\s+workflow/explore/agent-chat-eval\n`, out)
		assert.Regexp(t, `id:\s+explore-agent-chat-eval-`, out)
		assert.Regexp(t, `type:\s+explore \(from workflow/explore\)\n`, out)
		assert.Regexp(t, `next:\s+cd agent-chat-eval && fest create workflow agent-chat-eval\n`, out,
			"the cd target must work from the directory the command ran in")
		assert.NotContains(t, out, "—")

		assertMarkerType(t, "workflow/explore/agent-chat-eval", "explore")
		misplaced, err := tc.CheckDirExists(campaignDir + "/workflow/feature/agent-chat-eval")
		require.NoError(t, err)
		assert.False(t, misplaced, "inferred explore item must not land in workflow/feature")
	})

	t.Run("FromTypeDirectoryJSON", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir+"/workflow/design", "workitem", "create", "nav-redo", "--json")
		require.NoError(t, err, "create --json from workflow/design: %s", out)
		payload := decodeWorkitemCreateInfer(t, out)
		assert.Equal(t, "design", payload.Workitem.Type)
		assert.Equal(t, "workflow/design/nav-redo", payload.Workitem.RelativePath)
		assert.Equal(t, "fest create workflow nav-redo", payload.Next.Command)
		assert.Equal(t, "workflow/design/nav-redo", payload.Next.Cwd, "JSON next.cwd stays camp-relative")
		assertMarkerType(t, "workflow/design/nav-redo", "design")
	})

	t.Run("FromInsideExistingWorkitemCreatesSibling", func(t *testing.T) {
		tc.Shell(t, "mkdir -p "+campaignDir+"/workflow/explore/first-spike/notes")
		nested := campaignDir + "/workflow/explore/first-spike/notes"

		out, err := tc.RunCampInDir(nested, "workitem", "create", "second-spike", "--json")
		require.NoError(t, err, "create --json from inside a workitem: %s", out)
		payload := decodeWorkitemCreateInfer(t, out)
		assert.Equal(t, "explore", payload.Workitem.Type)
		assert.Equal(t, "workflow/explore/second-spike", payload.Workitem.RelativePath)
		assertMarkerType(t, "workflow/explore/second-spike", "explore")

		out, err = tc.RunCampInDir(nested, "workitem", "create", "third-spike")
		require.NoError(t, err, "create from inside a workitem: %s", out)
		t.Logf("human output from workflow/explore/first-spike/notes:\n%s", out)
		assert.Regexp(t, `path:\s+workflow/explore/third-spike\n`, out)
		assert.Regexp(t, `next:\s+cd \.\./\.\./third-spike && fest create workflow third-spike\n`, out)
		assertMarkerType(t, "workflow/explore/third-spike", "explore")

		for _, slug := range []string{"second-spike", "third-spike"} {
			nestedItem, err := tc.CheckDirExists(campaignDir + "/workflow/explore/first-spike/" + slug)
			require.NoError(t, err)
			assert.False(t, nestedItem, "%s must be a sibling, never nested inside first-spike", slug)
			nestedInNotes, err := tc.CheckDirExists(nested + "/" + slug)
			require.NoError(t, err)
			assert.False(t, nestedInNotes, "%s must not be created under the cwd", slug)
		}
	})

	t.Run("ThroughSymlinkedCampRoot", func(t *testing.T) {
		link := campaignDir + "-link"
		tc.Shell(t, "rm -f "+link+" && ln -s "+campaignDir+" "+link)

		out, err := tc.RunCampInDir(link+"/workflow/explore", "workitem", "create", "via-link", "--json")
		require.NoError(t, err, "create --json through a symlinked camp root: %s", out)
		payload := decodeWorkitemCreateInfer(t, out)
		assert.Equal(t, "explore", payload.Workitem.Type)
		assert.Equal(t, "workflow/explore/via-link", payload.Workitem.RelativePath)
		assertMarkerType(t, "workflow/explore/via-link", "explore")
	})

	t.Run("ThroughSymlinkBelowTheRootToOutsideTheCamp", func(t *testing.T) {
		external := campaignDir + "-external-notes"
		tc.Shell(t, "rm -rf "+external+" && mkdir -p "+external+" "+campaignDir+"/workflow/explore/linked-first && ln -sfn "+external+" "+campaignDir+"/workflow/explore/linked-first/notes")

		out, err := tc.RunCampInDir(campaignDir+"/workflow/explore/linked-first/notes", "workitem", "create", "linked-second", "--json")
		require.NoError(t, err, "create --json from a symlink below the root: %s", out)
		payload := decodeWorkitemCreateInfer(t, out)
		assert.Equal(t, "explore", payload.Workitem.Type)
		assert.Equal(t, "workflow/explore/linked-second", payload.Workitem.RelativePath)
		assertMarkerType(t, "workflow/explore/linked-second", "explore")
	})

	t.Run("ThroughInternalSymlinkKeepsLogicalPath", func(t *testing.T) {
		tc.Shell(t, "mkdir -p "+campaignDir+"/workflow/design/alias-target && ln -sfn ../design/alias-target "+campaignDir+"/workflow/explore/alias")

		out, err := tc.RunCampInDir(campaignDir+"/workflow/explore/alias", "workitem", "create", "alias-sibling")
		require.NoError(t, err, "create from an internal symlink: %s", out)
		assert.Contains(t, out, "Created explore workitem alias-sibling")
		assert.Regexp(t, `path:\s+workflow/explore/alias-sibling\n`, out)
		assert.Regexp(t, `next:\s+cd \.\./alias-sibling && fest create workflow alias-sibling\n`, out)
		assertMarkerType(t, "workflow/explore/alias-sibling", "explore")
	})

	t.Run("ExplicitTypeWins", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir+"/workflow/explore", "workitem", "create", "explicit-bug", "--type", "bug")
		require.NoError(t, err, "create --type bug from workflow/explore: %s", out)
		assert.Contains(t, out, "Created bug workitem explicit-bug")
		assert.Regexp(t, `path:\s+workflow/bug/explicit-bug\n`, out)
		assert.Regexp(t, `type:\s+bug\n`, out)
		assertMarkerType(t, "workflow/bug/explicit-bug", "bug")
	})

	t.Run("CampRootKeepsFeatureDefault", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "root-default")
		require.NoError(t, err, "create from camp root: %s", out)
		assert.Contains(t, out, "Created feature workitem root-default")
		assert.Regexp(t, `path:\s+workflow/feature/root-default\n`, out)
		assert.Regexp(t, `type:\s+feature \(default\)\n`, out)
		assertMarkerType(t, "workflow/feature/root-default", "feature")
	})

	t.Run("DirFlagInfersType", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir+"/workflow/explore", "workitem", "create", "dir-design", "--dir", "workflow/design", "--json")
		require.NoError(t, err, "create --dir workflow/design: %s", out)
		payload := decodeWorkitemCreateInfer(t, out)
		assert.Equal(t, "design", payload.Workitem.Type, "--dir wins over the cwd for inference")
		assert.Equal(t, "workflow/design/dir-design", payload.Workitem.RelativePath)
		assertMarkerType(t, "workflow/design/dir-design", "design")
	})

	t.Run("FileUnderTypeDirectory", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "--file", "workflow/bug/p99-notes.md")
		require.NoError(t, err, "create --file under workflow/bug: %s", out)
		t.Logf("human output for --file workflow/bug/p99-notes.md:\n%s", out)
		assert.Contains(t, out, "Created bug workitem p99-notes")
		assert.Regexp(t, `path:\s+workflow/bug/p99-notes\.md\n`, out)
		assert.Regexp(t, `type:\s+bug \(from workflow/bug\)\n`, out)

		content, err := tc.ReadFile(campaignDir + "/workflow/bug/p99-notes.md")
		require.NoError(t, err)
		assert.Contains(t, content, "kind: workitem")
		assert.Contains(t, content, "type: bug\n")
	})
}
