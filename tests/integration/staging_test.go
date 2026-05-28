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

// initStagingCampaign mirrors the prior unit-test stagingTestCampaign helper
// but seeds the fixture inside the shared container. The campaign contains
// one design workitem under workflow/design/example/ and a git repo at the
// campaign root.
func initStagingCampaign(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	_, err := tc.RunCamp(
		"init", dir,
		"--name", "Staging Test",
		"--type", "product",
		"-d", "Staging integration",
		"-m", "Verify ComputePlan via CLI",
		"--force", "--no-register", "--no-git",
	)
	require.NoError(t, err, "camp init")
	require.NoError(t, tc.CreateGitRepo(dir))

	const marker = `version: v1alpha6
kind: workitem
id: design-example-2026-05-24
type: design
title: Example
ref: WI-abc123
`
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/.workitem", marker))
	_, _, err = tc.ExecCommand("git", "-C", dir, "add", "-A")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("git", "-C", dir, "commit", "-q", "-m", "initial")
	require.NoError(t, err)
}

type planJSON struct {
	SchemaVersion string   `json:"schema_version"`
	Workitem      string   `json:"workitem"`
	Ref           string   `json:"workitem_ref"`
	FestivalRef   string   `json:"festival_ref"`
	Tag           string   `json:"tag"`
	Context       string   `json:"context"`
	ContextNote   string   `json:"context_note"`
	RepoRoot      string   `json:"repo_root"`
	Stage         []string `json:"stage"`
}

func runStagingPlan(t *testing.T, tc *TestContainer, dir string, args ...string) planJSON {
	t.Helper()
	full := append([]string{"workitem", "commit", "--json", "-m", "x"}, args...)
	out, err := tc.RunCampInDir(dir, full...)
	require.NoError(t, err, "workitem commit --json: %s", out)
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in output: %s", out)
	var plan planJSON
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &plan), "parse: %s", out[start:])
	return plan
}

func TestIntegration_ComputePlan_WorkitemDirAncestor(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-ancestor"
	initStagingCampaign(t, tc, dir)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/notes.md", "hi\n"))

	plan := runStagingPlan(t, tc, dir+"/workflow/design/example")
	assert.Equal(t, "workitem directory", plan.Context, "context: %+v", plan)
	assert.Contains(t, plan.Stage, "workflow/design/example/notes.md", "stage missing notes.md: %+v", plan)
	assert.Contains(t, plan.Tag, "WI-WI-abc123", "tag missing ref: %s", plan.Tag)
}

func TestIntegration_ComputePlan_AutoIncludedLinkRegistryAnnotated(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-link-registry"
	initStagingCampaign(t, tc, dir)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/notes.md", "notes\n"))
	require.NoError(t, tc.WriteFile(
		dir+"/.campaign/workitems/links.yaml",
		"version: workitem-links/v1alpha1\nlinks: []\n"))

	out, err := tc.RunCampInDir(dir+"/workflow/design/example",
		"workitem", "commit", "-m", "notes")
	require.NoError(t, err, "workitem commit: %s", out)
	assert.Contains(t, out, ".campaign/workitems/links.yaml (link registry auto-included)",
		"plan output must annotate the auto-included registry: %s", out)
}

func TestIntegration_ComputePlan_NoContextErrors(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-no-context"
	initStagingCampaign(t, tc, dir)

	out, _, err := tc.ExecCommand("sh", "-c",
		"cd "+dir+" && /camp workitem commit -m 'no ctx' 2>&1; echo EXIT=$?")
	require.NoError(t, err)
	assert.Contains(t, out, "no workitem context",
		"expected ErrNoWorkitemContext surface: %s", out)
	assert.NotContains(t, out, "EXIT=0",
		"commit must refuse from campaign root with no workitem in scope: %s", out)
}

func TestIntegration_ComputePlan_ExplicitWorkitem(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-explicit"
	initStagingCampaign(t, tc, dir)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/spec.md", "spec\n"))

	plan := runStagingPlan(t, tc, dir,
		"--workitem", "design-example-2026-05-24")
	assert.Equal(t, "campaign root", plan.Context, "context: %+v", plan)
	assert.Contains(t, plan.Stage, "workflow/design/example/spec.md", "stage: %+v", plan)
}

func TestIntegration_ComputePlan_ExcludeRemovesPath(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-exclude"
	initStagingCampaign(t, tc, dir)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/a.md", "a\n"))
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/b.md", "b\n"))

	plan := runStagingPlan(t, tc, dir+"/workflow/design/example",
		"--exclude", "workflow/design/example/b.md")
	assert.Contains(t, plan.Stage, "workflow/design/example/a.md", "stage: %+v", plan)
	assert.NotContains(t, plan.Stage, "workflow/design/example/b.md", "exclude failed: %+v", plan)
}

func TestIntegration_ComputePlan_IncludeAddsExplicitPath(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-include"
	initStagingCampaign(t, tc, dir)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/notes.md", "notes\n"))
	require.NoError(t, tc.WriteFile(dir+"/docs/extra.md", "extra\n"))

	plan := runStagingPlan(t, tc, dir+"/workflow/design/example",
		"--include", "docs/extra.md")
	assert.Contains(t, plan.Stage, "workflow/design/example/notes.md", "stage: %+v", plan)
	assert.Contains(t, plan.Stage, "docs/extra.md", "include not honored: %+v", plan)
}

func TestIntegration_ComputePlan_StagedShortCircuits(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-staged"
	initStagingCampaign(t, tc, dir)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/notes.md", "x\n"))
	_, _, err := tc.ExecCommand("git", "-C", dir, "add", "workflow/design/example/notes.md")
	require.NoError(t, err)

	plan := runStagingPlan(t, tc, dir,
		"--workitem", "design-example-2026-05-24", "--staged")
	assert.Equal(t, "staged-only", plan.Context, "context: %+v", plan)
}

func TestIntegration_ComputePlan_LinkedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-linked"
	initStagingCampaign(t, tc, dir)

	projDir := dir + "/projects/demo"
	require.NoError(t, tc.CreateGitRepo(projDir))
	_, err := tc.RunCampInDir(dir, "workitem", "link",
		"design-example-2026-05-24", "--project", "demo")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(projDir+"/foo.go", "package x\n"))
	plan := runStagingPlan(t, tc, projDir)
	assert.Equal(t, "linked project", plan.Context, "context: %+v", plan)
	assert.Contains(t, plan.RepoRoot, "projects/demo", "repo_root: %+v", plan)
}

func TestIntegration_ComputePlan_ProjectFlagOverridesResolver(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/staging-project-flag"
	initStagingCampaign(t, tc, dir)

	require.NoError(t, tc.CreateGitRepo(dir+"/projects/demo"))
	require.NoError(t, tc.WriteFile(dir+"/projects/demo/x.go", "package x\n"))

	plan := runStagingPlan(t, tc, dir+"/workflow/design/example",
		"--project", "demo")
	assert.Equal(t, "linked project", plan.Context, "context: %+v", plan)
	assert.Contains(t, plan.RepoRoot, "projects/demo", "repo_root: %+v", plan)
}
