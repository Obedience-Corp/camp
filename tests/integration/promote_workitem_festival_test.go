//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Promoting a design workitem to a festival must migrate its primary worktree
// link onto the festival (so doctor stays clean) and switch the linked
// worktree's commit tag from WI-<design ref> to FE-<festival ref>, instead of
// orphaning the link and dropping the tag entirely.
func TestIntegration_PromoteWorkitemToFestival_MigratesLinkAndTagsFE(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available in container")
	}
	tc := GetSharedContainer(t)
	path := "/campaigns/promote-wi-festival"

	_, err := tc.InitCampaign(path, "promote-wi-festival", "product")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"mkdir -p %s/festivals/.festival/templates %s/festivals/.festival/.state %s/festivals/planning",
		path, path, path))
	require.NoError(t, err)

	slug := "sync-clone-transport"
	ref := seedDesignWorkitemWithRef(t, tc, path, slug)
	require.NoError(t, tc.WriteFile(path+"/workflow/design/"+slug+"/README.md",
		"# "+slug+"\n\nBody paragraph so the festival promote is not empty.\n"))

	_, err = tc.RunCampInDir(path, "project", "new", "demo-app")
	require.NoError(t, err, "camp project new")
	_, err = tc.RunCampInDir(path, "project", "worktree", "add", "sync-wt",
		"--project", "demo-app", "--workitem", slug)
	require.NoError(t, err, "worktree add --workitem")
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add -A && git commit -m 'setup' --allow-empty")
	require.NoError(t, err)

	wtRel := "projects/worktrees/demo-app/sync-wt"

	// Before promote: a commit in the worktree carries the design WI-<ref>.
	require.NoError(t, tc.WriteFile(path+"/"+wtRel+"/before.go", "package x\n"))
	beforeOut, err := tc.RunCampInDir(path+"/"+wtRel, "p", "commit", "-m", "feat: before promote")
	require.NoError(t, err, "camp p commit (before): %s", beforeOut)
	assert.Contains(t, lastCommitSubject(t, tc, path+"/"+wtRel), ref,
		"pre-promote commit should carry the design WI-<ref>")

	// Promote the design workitem to a festival.
	promoteOut, err := tc.RunCampInDir(path, "workitem", "promote", slug, "--target", "festival")
	require.NoError(t, err, "camp workitem promote --target festival: %s", promoteOut)

	festDir := findPlanningFestivalDir(t, tc, path, slug, promoteOut)
	festID := festDir[strings.LastIndex(festDir, "-")+1:]
	require.NotEmpty(t, festID, "could not derive festival id from dir %q", festDir)

	// The primary link is migrated onto the festival's single-segment id.
	linksYAML, err := tc.ReadFile(path + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksYAML, "workitem_id: "+festID,
		"link should be migrated to the festival id; links.yaml:\n%s", linksYAML)

	// Doctor is clean: the migrated link resolves, so no broken-link finding.
	doctorOut, err := tc.RunCampInDir(path, "workitem", "doctor")
	require.NoError(t, err, "doctor should exit clean: %s", doctorOut)
	assert.Contains(t, doctorOut, "0 findings", "doctor should report no findings: %s", doctorOut)

	// After promote: a commit in the same worktree carries FE-<festival ref>
	// and no longer the retired design WI-<ref>.
	require.NoError(t, tc.WriteFile(path+"/"+wtRel+"/after.go", "package x\n\n// more\n"))
	afterOut, err := tc.RunCampInDir(path+"/"+wtRel, "p", "commit", "-m", "feat: after promote")
	require.NoError(t, err, "camp p commit (after): %s", afterOut)
	subject := lastCommitSubject(t, tc, path+"/"+wtRel)
	assert.Contains(t, subject, "FE-"+festID,
		"post-promote worktree commit should carry FE-<festival ref>: %s", subject)
	assert.NotContains(t, subject, ref,
		"post-promote commit should not carry the retired design WI-<ref>: %s", subject)

	// Old-orphaned-state recovery: point the link back at the (dungeoned)
	// design id to simulate a link orphaned by a pre-fix promote, then confirm
	// doctor detects the promotion and --fix re-points instead of deleting.
	designID := readDungeonedDesignID(t, tc, path)
	require.NotEmpty(t, designID, "could not read dungeoned design id")
	_, _, err = tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"sed -i 's|workitem_id: %s|workitem_id: %s|' %s/.campaign/workitems/links.yaml",
		festID, designID, path))
	require.NoError(t, err)

	orphanDoctor, _ := tc.RunCampInDir(path, "workitem", "doctor")
	assert.Contains(t, orphanDoctor, "promoted to festival "+festID,
		"doctor should detect the promotion, not just offer deletion: %s", orphanDoctor)

	fixOut, err := tc.RunCampInDir(path, "workitem", "doctor", "--fix")
	require.NoError(t, err, "doctor --fix: %s", fixOut)
	linksAfterFix, err := tc.ReadFile(path + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksAfterFix, "workitem_id: "+festID,
		"doctor --fix should re-point the orphaned link to the festival, not delete it:\n%s", linksAfterFix)
}

// readDungeonedDesignID reads the stable id from the shelved design workitem's
// marker under the (hidden) design dungeon.
func readDungeonedDesignID(t *testing.T, tc *TestContainer, path string) string {
	t.Helper()
	out, _, err := tc.ExecCommand("sh", "-c",
		"grep -h -m1 '^id:' \"$(find "+path+"/workflow/design -type f -name .workitem | head -1)\"")
	require.NoError(t, err)
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "id:"))
}
