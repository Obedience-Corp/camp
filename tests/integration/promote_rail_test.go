//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRailCampaign creates a campaign holding one committed design workitem,
// the starting point for every rail move.
func setupRailCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(path,
		"workitem", "create", "rail-feature",
		"--type", "design",
		"--title", "Rail feature",
		"--id", "design-rail-feature-fixed",
	)
	require.NoError(t, err, "workitem create should succeed: %s", out)

	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add -A && git commit -m 'add rail workitem'")
	require.NoError(t, err)

	return path
}

// railTo promotes the campaign's workitem onto a stage from the given cwd.
func railTo(t *testing.T, tc *TestContainer, cwd, stage string) string {
	t.Helper()
	out, err := tc.RunCampInDir(cwd, "workitem", "promote", "--target", stage)
	require.NoError(t, err, "promote to %s should succeed: %s", stage, out)
	return out
}

// TestPromoteRail_RootToStage covers both rail-entry moves. The directory itself
// relocates, so the assertions are physical: the destination exists with its
// content and marker, the source is gone, and the move is committed.
func TestPromoteRail_RootToStage(t *testing.T) {
	for _, stage := range []string{"ready", "active"} {
		t.Run(stage, func(t *testing.T) {
			tc := GetSharedContainer(t)
			path := setupRailCampaign(t, tc, "rail-entry-"+stage)

			out := railTo(t, tc, path+"/workflow/design/rail-feature", stage)
			assert.Contains(t, out, "festivals/"+stage+"/rail-feature")

			exists, err := tc.CheckDirExists(path + "/festivals/" + stage + "/rail-feature")
			require.NoError(t, err)
			assert.True(t, exists, "workitem should live on the %s stage", stage)

			exists, err = tc.CheckFileExists(path + "/festivals/" + stage + "/rail-feature/.workitem")
			require.NoError(t, err)
			assert.True(t, exists, "marker must travel with the resident or it cannot be resolved again")

			exists, err = tc.CheckDirExists(path + "/workflow/design/rail-feature")
			require.NoError(t, err)
			assert.False(t, exists, "source should be gone; the move is the promotion")

			status, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
			require.NoError(t, err)
			assert.Empty(t, strings.TrimSpace(status), "rail move should auto-commit and leave a clean tree")
		})
	}
}

// TestPromoteRail_ReadyToActiveAdvance exercises the advance, whose source is a
// resident and therefore depends on resident location resolution.
func TestPromoteRail_ReadyToActiveAdvance(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "rail-advance")

	railTo(t, tc, path+"/workflow/design/rail-feature", "ready")
	out := railTo(t, tc, path+"/festivals/ready/rail-feature", "active")
	assert.Contains(t, out, "festivals/active/rail-feature")

	exists, err := tc.CheckDirExists(path + "/festivals/active/rail-feature")
	require.NoError(t, err)
	assert.True(t, exists, "resident should have advanced to active")

	exists, err = tc.CheckDirExists(path + "/festivals/ready/rail-feature")
	require.NoError(t, err)
	assert.False(t, exists, "resident should no longer be on ready")

	status, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(status), "advance should auto-commit")
}

// TestPromoteRail_ForwardOnlyRejections locks every refused transition. A
// rejection must change nothing on disk: the workitem stays exactly where it was.
func TestPromoteRail_ForwardOnlyRejections(t *testing.T) {
	tests := []struct {
		name     string
		stageNow string // rail stage to park the workitem on first, "" = stay at root
		shelve   bool   // shelve into the dungeon instead
		target   string
		wantErr  string
	}{
		{name: "active to ready", stageNow: "active", target: "ready", wantErr: "forward-only"},
		{name: "ready to ready", stageNow: "ready", target: "ready", wantErr: "forward-only"},
		{name: "active to active", stageNow: "active", target: "active", wantErr: "already active on the rail"},
		{name: "dungeon to ready", shelve: true, target: "ready", wantErr: "restoring workitems out of the dungeon is not a promote"},
		{name: "dungeon to active", shelve: true, target: "active", wantErr: "restoring workitems out of the dungeon is not a promote"},
	}

	for _, tc2 := range tests {
		t.Run(tc2.name, func(t *testing.T) {
			tc := GetSharedContainer(t)
			path := setupRailCampaign(t, tc, "rail-reject-"+strings.ReplaceAll(tc2.name, " ", "-"))

			var srcDir string
			switch {
			case tc2.shelve:
				out, err := tc.RunCampInDir(path+"/workflow/design/rail-feature", "workitem", "promote", "--target", "completed")
				require.NoError(t, err, "shelve should succeed: %s", out)
				found, _, ferr := tc.ExecCommand("sh", "-c",
					"find "+path+"/workflow/design/dungeon -type d -name rail-feature | head -1")
				require.NoError(t, ferr)
				srcDir = strings.TrimSpace(found)
				require.NotEmpty(t, srcDir, "shelved workitem should be findable in the dungeon")
			case tc2.stageNow != "":
				railTo(t, tc, path+"/workflow/design/rail-feature", tc2.stageNow)
				srcDir = path + "/festivals/" + tc2.stageNow + "/rail-feature"
			default:
				srcDir = path + "/workflow/design/rail-feature"
			}

			_, stderr, exitCode, err := tc.RunCampSplitInDir(srcDir, "workitem", "promote", "--target", tc2.target)
			require.NoError(t, err)
			assert.NotZero(t, exitCode, "a refused rail move must exit non-zero")
			assert.Contains(t, stderr, tc2.wantErr)

			exists, cerr := tc.CheckDirExists(srcDir)
			require.NoError(t, cerr)
			assert.True(t, exists, "a refused promote must leave the workitem where it was")

			status, _, serr := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
			require.NoError(t, serr)
			assert.Empty(t, strings.TrimSpace(status), "a refused promote must not dirty the tree")
		})
	}
}

// TestPromoteRail_RepairsReferences asserts the fixup surface doc 04 rule 3
// requires: the links registry re-homes onto the new path, markdown
// cross-references are rewritten, and an audit event is appended.
func TestPromoteRail_RepairsReferences(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "rail-references")

	require.NoError(t, tc.WriteFile(path+"/docs/index.md",
		"# Index\n\nSee [the item](../workflow/design/rail-feature/README.md).\n"))
	// The link scope must exist on disk before it can be linked.
	_, _, err := tc.ExecCommand("sh", "-c", "mkdir -p "+path+"/projects/worktrees/demo")
	require.NoError(t, err)
	out, err := tc.RunCampInDir(path, "workitem", "link", "design-rail-feature-fixed",
		"projects/worktrees/demo", "--role", "primary")
	require.NoError(t, err, "link should succeed: %s", out)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add -A && git commit -m 'add refs'")
	require.NoError(t, err)

	railTo(t, tc, path+"/workflow/design/rail-feature", "ready")

	linksBody, err := tc.ReadFile(path + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksBody, "design:festivals/ready/rail-feature",
		"link key should re-home onto the resident path")
	assert.NotContains(t, linksBody, "design:workflow/design/rail-feature",
		"the old key must not survive")
	assert.Contains(t, linksBody, "projects/worktrees/demo",
		"the link's scope must be preserved, only its key moves")

	indexBody, err := tc.ReadFile(path + "/docs/index.md")
	require.NoError(t, err)
	assert.Contains(t, indexBody, "../festivals/ready/rail-feature/README.md",
		"markdown cross-reference should follow the move")

	audit, err := tc.ReadFile(path + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err)
	assert.Contains(t, audit, "festivals/ready/rail-feature", "a promote event should record the destination")
}

// TestPromoteRail_PriorityStoreRekeyedNotStaged mirrors the rename expectation:
// .campaign/settings/workitems.json is gitignored per-machine state, so a rail
// move must re-key it on disk without dragging it into the commit.
func TestPromoteRail_PriorityStoreRekeyedNotStaged(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "rail-priority")

	out, err := tc.RunCampInDir(path, "workitem", "priority",
		"design-rail-feature-fixed", "high")
	require.NoError(t, err, "priority set should succeed: %s", out)

	before, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	require.Contains(t, before, "design:workflow/design/rail-feature",
		"priority store should be keyed on the pre-move path")

	railTo(t, tc, path+"/workflow/design/rail-feature", "ready")

	after, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	assert.Contains(t, after, "design:festivals/ready/rail-feature",
		"priority store should be re-keyed onto the resident path")
	assert.NotContains(t, after, "design:workflow/design/rail-feature",
		"the old priority key must not survive")

	files := tc.GitOutput(t, path, "show", "--name-only", "--format=", "HEAD")
	assert.NotContains(t, files, ".campaign/settings/workitems.json",
		"the priority store is gitignored machine state and must not be staged")
}

func TestPromoteRail_RepairsQuestLinks(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "rail-quest-links")

	out, err := tc.RunCampInDir(path, "quest", "create", "rail-quest",
		"--no-editor", "--workitem", "design-rail-feature-fixed")
	require.NoError(t, err, "quest create should succeed: %s", out)

	railTo(t, tc, path+"/workflow/design/rail-feature", "ready")

	linksOut, err := tc.RunCampInDir(path, "quest", "links", "rail-quest", "--json")
	require.NoError(t, err, "quest links: %s", linksOut)
	assert.Contains(t, linksOut, "festivals/ready/rail-feature")
	assert.NotContains(t, linksOut, `"path": "workflow/design/rail-feature"`)

	status, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(status), "rewritten quest.yaml must land in the promote commit")
}

// TestPromoteRail_StampsUnmarkedSource is why stamping happens at rail entry.
// Directory workitems are not uniformly marked, but a resident without a marker
// cannot be resolved, so entering the rail unmarked would strand it.
func TestPromoteRail_StampsUnmarkedSource(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/rail-unmarked"
	_, err := tc.InitCampaign(path, "rail-unmarked", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(path+"/workflow/design/bare/README.md", "# Bare\n\nNo marker.\n"))
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add -A && git commit -m 'add bare dir'")
	require.NoError(t, err)

	exists, err := tc.CheckFileExists(path + "/workflow/design/bare/.workitem")
	require.NoError(t, err)
	require.False(t, exists, "fixture must start without a marker")

	railTo(t, tc, path+"/workflow/design/bare", "ready")

	marker, err := tc.ReadFile(path + "/festivals/ready/bare/.workitem")
	require.NoError(t, err, "rail entry must mint a marker")
	assert.Contains(t, marker, "type: design", "minted marker keeps the source's workflow type")
	assert.Contains(t, marker, "kind: workitem")
	assert.Regexp(t, `(?m)^id: .+$`, marker, "minted marker must carry an id")

	// The marker is what makes the resident resolvable, so a second rail move
	// off it must work.
	railTo(t, tc, path+"/festivals/ready/bare", "active")
	exists, err = tc.CheckDirExists(path + "/festivals/active/bare")
	require.NoError(t, err)
	assert.True(t, exists, "a stamped resident must be able to advance")
}

// TestPromoteRail_ExploreAcceptsReady confirms an explore workitem reaches the
// ready stage. Reclassification in camp wi is sequence 03; this asserts only that
// the promote succeeds and the directory lands on the stage.
func TestPromoteRail_ExploreAcceptsReady(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/rail-explore"
	_, err := tc.InitCampaign(path, "rail-explore", "product")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(path,
		"workitem", "create", "explore-topic",
		"--type", "explore",
		"--title", "Explore topic",
		"--id", "explore-topic-fixed",
	)
	require.NoError(t, err, "explore create should succeed: %s", out)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add -A && git commit -m 'add explore'")
	require.NoError(t, err)

	railTo(t, tc, path+"/workflow/explore/explore-topic", "ready")

	exists, err := tc.CheckDirExists(path + "/festivals/ready/explore-topic")
	require.NoError(t, err)
	assert.True(t, exists, "explore workitem should reach the ready stage")

	marker, err := tc.ReadFile(path + "/festivals/ready/explore-topic/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "type: explore", "a resident keeps its original workflow type")
}

// TestPromoteRail_RepairsMismatchedMarkerType covers a valid marker whose type
// disagrees with the workflow path. The path type is authoritative: reference
// migration keys the move on it, while resident resolution afterwards reads the
// marker, so an unrepaired mismatch would re-home priority onto a key no rail
// command can resolve. Rail entry must canonicalize the marker before moving.
func TestPromoteRail_RepairsMismatchedMarkerType(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "rail-mismarked")

	out, err := tc.RunCampInDir(path, "workitem", "priority",
		"design-rail-feature-fixed", "high")
	require.NoError(t, err, "priority set should succeed: %s", out)

	_, _, err = tc.ExecCommand("sh", "-c",
		"sed -i 's/^type: design$/type: explore/' "+path+"/workflow/design/rail-feature/.workitem"+
			" && cd "+path+" && git add -A && git commit -m 'mismark type'")
	require.NoError(t, err)
	fixture, err := tc.ReadFile(path + "/workflow/design/rail-feature/.workitem")
	require.NoError(t, err)
	require.Contains(t, fixture, "type: explore", "fixture must start with the mismatched type")

	railTo(t, tc, path+"/workflow/design/rail-feature", "ready")

	marker, err := tc.ReadFile(path + "/festivals/ready/rail-feature/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "type: design", "the marker must be repaired to the authoritative path type")
	assert.NotContains(t, marker, "type: explore", "the mismatched type must not survive rail entry")
	assert.Contains(t, marker, "id: design-rail-feature-fixed", "repair must preserve the existing id")

	priorities, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	assert.Contains(t, priorities, "design:festivals/ready/rail-feature",
		"the migrated priority key and the repaired marker must agree on the type")

	// The proof the fracture is gone: resident resolution reads the repaired
	// marker, so the same workitem must resolve and advance.
	railTo(t, tc, path+"/festivals/ready/rail-feature", "active")
	after, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	assert.Contains(t, after, "design:festivals/active/rail-feature",
		"the advance must re-key the same priority entry again")
}

// TestPromoteRail_MalformedMarkerFailsBeforeMoving covers the marker the repair
// planner refuses: unparseable YAML. Moving first would strand a resident
// nothing can resolve, so the promote must fail with the source untouched.
func TestPromoteRail_MalformedMarkerFailsBeforeMoving(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "rail-malformed")

	require.NoError(t, tc.WriteFile(path+"/workflow/design/rail-feature/.workitem",
		"kind: [unclosed\n"))
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git add -A && git commit -m 'corrupt marker'")
	require.NoError(t, err)

	_, stderr, exitCode, err := tc.RunCampSplitInDir(path+"/workflow/design/rail-feature",
		"workitem", "promote", "--target", "ready")
	require.NoError(t, err)
	assert.NotZero(t, exitCode, "a promote with an unparseable marker must fail")
	assert.Contains(t, stderr, "cannot parse existing .workitem",
		"the failure must name the marker so the operator knows what to fix")

	exists, err := tc.CheckDirExists(path + "/workflow/design/rail-feature")
	require.NoError(t, err)
	assert.True(t, exists, "failing before the move leaves the workitem in place")

	exists, err = tc.CheckDirExists(path + "/festivals/ready/rail-feature")
	require.NoError(t, err)
	assert.False(t, exists, "nothing may land on the stage when the marker cannot be repaired")

	status, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(status), "a refused promote must not dirty the tree")
}

// TestPromoteRail_RejectsOccupiedDestination guards against clobbering a
// resident that already holds the slug on that stage.
func TestPromoteRail_RejectsOccupiedDestination(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "rail-occupied")

	require.NoError(t, tc.WriteFile(path+"/festivals/ready/rail-feature/README.md", "# Squatter\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/ready/rail-feature/.workitem",
		"version: v1alpha8\nkind: workitem\nid: design-squatter-fixed\ntype: design\ntitle: Squatter\n"))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(path+"/workflow/design/rail-feature",
		"workitem", "promote", "--target", "ready")
	require.NoError(t, err)
	assert.NotZero(t, exitCode, "promoting onto an occupied slug must fail")
	assert.Contains(t, stderr, "already exists")

	body, err := tc.ReadFile(path + "/festivals/ready/rail-feature/README.md")
	require.NoError(t, err)
	assert.Contains(t, body, "Squatter", "the occupant must be untouched")

	exists, err := tc.CheckDirExists(path + "/workflow/design/rail-feature")
	require.NoError(t, err)
	assert.True(t, exists, "the refused source must stay in place")
}
