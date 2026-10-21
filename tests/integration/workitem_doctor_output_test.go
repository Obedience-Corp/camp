//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemDoctor_ReadableReportPreservesLinks(t *testing.T) {
	tc := GetSharedContainer(t)
	root := "/test/doctor-readable"
	initLinksCampaign(t, tc, root)
	seedProject(t, tc, root, "activity")
	out, err := tc.RunCampInDir(root, "workitem", "create", "color-profiles", "--type", "design", "--title", "Shared color profiles", "--project", "projects/immerse", "--json")
	require.NoError(t, err, out)
	var item struct {
		Workitem struct {
			ID string `json:"id"`
		} `json:"workitem"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &item), out)
	require.NotEmpty(t, item.Workitem.ID, out)
	body := "version: workitem-links/v1alpha1\nlinks:\n"
	for i, name := range []string{"commerce", "keys", "release", "release-fix", "old-branch"} {
		project := "projects/timeline"
		if i == 4 {
			project = "projects/activity"
		}
		body += fmt.Sprintf(`  - id: lnk_20260908_%06d
    workitem_id: %s
    workitem_key: design:workflow/design/color-profiles
    scope:
      kind: worktree
      path: projects/worktrees/activity/%s
      project: %s
    role: related
    created_at: 2026-09-08T00:00:00Z
    created_by: test
`, i, item.Workitem.ID, name, project)
	}
	require.NoError(t, tc.WriteFile(root+"/.campaign/workitems/links.yaml", body))
	before, err := tc.ReadFile(root + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	out, err = tc.RunCampInDir(root, "workitem", "doctor", "--fix", "--no-color")
	require.NoError(t, err, out)
	assert.Contains(t, out, "0 errors, 5 warnings, 1 informational summary")
	assert.Contains(t, out, "Unavailable worktrees: 4 links")
	assert.Contains(t, out, "Parent project also unavailable: projects/timeline")
	assert.Contains(t, out, "No action needed")
	assert.Contains(t, out, "1 worktree link preserved")
	assert.Contains(t, out, "No automatic repairs available for the remaining findings.")
	assert.NotContains(t, out, "\x1b[")
	assert.NotContains(t, out, "workitem.scope.not-on-this-machine")
	assert.NotContains(t, out, "lnk_20260908_")
	after, err := tc.ReadFile(root + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Equal(t, before, after, "presentation must not rewrite unresolved links")
	out, err = tc.RunCampInDir(root, "workitem", "doctor", "--verbose", "--no-color")
	require.NoError(t, err, out)
	assert.Contains(t, out, "camp workitem unlink --id lnk_20260908_000000")
	assert.Contains(t, out, "workitem.scope.not-on-this-machine")
	out, err = tc.RunCampInDir(root, "workitem", "doctor", "--json")
	require.NoError(t, err, out)
	findings := doctorFindings(t, out)
	require.Len(t, findings, 6)
	for _, f := range findings {
		assert.False(t, f.AutoFixable)
	}
}
