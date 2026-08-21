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

func TestIntegration_WorkitemDoctor_RewritesStaleProjectAfterGitRename(t *testing.T) {
	tc := GetSharedContainer(t)
	root := "/campaigns/workitem-doctor-project-rename"
	_, err := tc.InitCampaign(root, "doctor-rename", "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		set -eu
		cd %[1]s
		mkdir -p projects/oldname
		printf 'x\n' > projects/oldname/README.md
		git add projects/oldname
		git commit -q -m 'add oldname'
	`, root))

	out, err := tc.RunCampInDir(root, "workitem", "create", "renamed-proj",
		"--type", "feature", "--title", "Renamed", "--project", "projects/oldname")
	require.NoError(t, err, "create directory workitem: %s", out)

	out, err = tc.RunCampInDir(root, "workitem", "create",
		"--file", "workflow/feature/renamed-file.md",
		"--type", "feature", "--title", "Renamed File", "--project", "projects/oldname")
	require.NoError(t, err, "create file workitem: %s", out)

	tc.Shell(t, fmt.Sprintf(`
		set -eu
		cd %[1]s
		git mv projects/oldname projects/newname
		git commit -q -m 'rename project'
	`, root))

	dout, derr := tc.RunCampInDir(root, "workitem", "doctor", "--json")
	require.NoError(t, derr, "doctor --json: %s", dout)
	var autoFixable int
	for _, f := range doctorFindings(t, dout) {
		if f.Code != "workitem.project.not-found" {
			continue
		}
		assert.True(t, f.AutoFixable, "rename-mapped missing project must be auto_fixable: %+v", f)
		assert.Contains(t, f.FixHint, "projects/newname")
		autoFixable++
	}
	assert.GreaterOrEqual(t, autoFixable, 2, "directory and file workitems should both warn:\n%s", dout)

	fout, ferr := tc.RunCampInDir(root, "workitem", "doctor", "--fix")
	require.NoError(t, ferr, "doctor --fix: %s", fout)

	marker, err := tc.ReadFile(root + "/workflow/feature/renamed-proj/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "projects/newname")
	assert.NotContains(t, marker, "projects/oldname")

	fileBody, err := tc.ReadFile(root + "/workflow/feature/renamed-file.md")
	require.NoError(t, err)
	assert.Contains(t, fileBody, "projects/newname")
	assert.NotContains(t, fileBody, "projects/oldname")

	dout2, derr2 := tc.RunCampInDir(root, "workitem", "doctor", "--json")
	require.NoError(t, derr2, "doctor after fix: %s", dout2)
	for _, f := range doctorFindings(t, dout2) {
		assert.NotEqual(t, "workitem.project.not-found", f.Code,
			"stale project should be gone after --fix: %+v\n%s", f, dout2)
	}
}

func TestIntegration_WorkitemDoctor_MissingProjectWithoutRenameStaysManual(t *testing.T) {
	tc := GetSharedContainer(t)
	root := "/campaigns/workitem-doctor-project-missing"
	_, err := tc.InitCampaign(root, "doctor-missing", "product")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(root, "workitem", "create", "ghost-proj",
		"--type", "feature", "--title", "Ghost", "--project", "projects/never-existed")
	require.NoError(t, err, "create: %s", out)

	dout, derr := tc.RunCampInDir(root, "workitem", "doctor", "--json")
	require.NoError(t, derr, "doctor --json: %s", dout)
	var found bool
	for _, f := range doctorFindings(t, dout) {
		if f.Code != "workitem.project.not-found" {
			continue
		}
		found = true
		assert.False(t, f.AutoFixable, "no git rename mapping must not guess: %+v", f)
		assert.True(t, strings.Contains(f.FixHint, "does not auto-remove") ||
			strings.Contains(f.FixHint, "renamed/removed"),
			"fix hint: %s", f.FixHint)
	}
	assert.True(t, found, "expected a missing-project finding:\n%s", dout)
}
