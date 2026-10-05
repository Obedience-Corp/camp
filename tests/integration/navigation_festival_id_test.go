//go:build integration

package integration

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGo_FestivalIDPrecedesProjectAndWorktree(t *testing.T) {
	tc := GetSharedContainer(t)
	root := "/campaigns/festival-id-precedence"
	_, err := tc.InitCampaign(root, "festival-id-precedence", "product")
	require.NoError(t, err)
	tc.Shell(t, "mkdir -p "+root+"/projects/FA0030 "+root+"/projects/api-service")
	require.NoError(t, tc.CreateGitRepo(root+"/projects/festival-app"))
	tc.Shell(t, "git -C "+root+"/projects/festival-app worktree add "+root+"/projects/worktrees/festival-app/fa0030-hooks")
	// Identity comes from metadata, even when a directory's suffix disagrees.
	want := writeNavigationFestival(t, tc, root, "active", "actual-festival-ZZ9999", "FA0030")
	writeNavigationFestival(t, tc, root, "planning", "misleading-festival-FA0030", "OT0001")
	resident := writeNavigationFestival(t, tc, root, "active", "camp-owned-resident", "FA0030")
	require.NoError(t, tc.WriteFile(filepath.Join(resident, ".workitem"), "{}\n"))
	for _, args := range [][]string{{"FA0030"}, {"fa0030"}, {"f", "FA0030"}, {"festivals/FA0030"}} {
		output, err := tc.RunCampInDir(root, append([]string{"go", "--print"}, args...)...)
		require.NoError(t, err, "exact festival identity should win: %s", output)
		require.Equal(t, want, strings.TrimSpace(output))
	}
	output, err := tc.RunCampInDir(root, "go", "p", "FA0030", "--print")
	require.NoError(t, err)
	require.Equal(t, root+"/projects/FA0030", strings.TrimSpace(output), "explicit project category must be respected")
	output, err = tc.RunCampInDir(root, "go", "p", "api", "--print")
	require.NoError(t, err)
	require.Equal(t, root+"/projects/api-service", strings.TrimSpace(output), "ordinary fuzzy navigation must remain available")
	output, err = tc.RunCampInDir(root, "go", "f", "misleading", "--print")
	require.NoError(t, err)
	require.Contains(t, output, "planning/misleading-festival-FA0030", "festival slug searches must retain fuzzy matching")
}

func TestGo_FestivalIDAcrossLifecycleAndAmbiguity(t *testing.T) {
	tc := GetSharedContainer(t)
	root := "/campaigns/festival-id-lifecycle"
	_, err := tc.InitCampaign(root, "festival-id-lifecycle", "product")
	require.NoError(t, err)
	stages := []string{"active", "ready", "planning", "ritual", "chains",
		".dungeon/completed", ".dungeon/archived", ".dungeon/someday"}
	for i, stage := range stages {
		id := fmt.Sprintf("FI%04d", i+1)
		want := writeNavigationFestival(t, tc, root, stage, "lifecycle-"+id, id)
		output, err := tc.RunCampInDir(root, "go", "--print", id)
		require.NoError(t, err, "festival in %s should resolve: %s", stage, output)
		require.Equal(t, want, strings.TrimSpace(output))
	}
	for _, id := range []string{"FI10000", "RI-FI0001"} {
		want := writeNavigationFestival(t, tc, root, "ritual", "extended-"+id, id)
		output, err := tc.RunCampInDir(root, "go", "--print", id)
		require.NoError(t, err, "extended canonical IDs should resolve: %s", output)
		require.Equal(t, want, strings.TrimSpace(output))
	}
	writeNavigationFestival(t, tc, root, "planning", "duplicate-FI0001", "FI0001")
	output, err := tc.RunCampInDir(root, "go", "--print", "FI0001")
	require.Error(t, err, "duplicate identifiers must fail instead of choosing a fuzzy target")
	require.Contains(t, output, "festival ID FI0001 is ambiguous")
	require.Contains(t, output, "active/lifecycle-FI0001")
	require.Contains(t, output, "planning/duplicate-FI0001")
	output, err = tc.RunCampInDir(root, "go", "--print", "ZZ9998")
	require.Error(t, err, "an unknown identifier must not invent a destination")
	require.Contains(t, output, "no targets matching")
}

func writeNavigationFestival(t *testing.T, tc *TestContainer, root, stage, name, id string) string {
	t.Helper()
	path := filepath.Join(root, "festivals", stage, name)
	_, _, err := tc.ExecCommand("mkdir", "-p", path)
	require.NoError(t, err)
	require.NoError(t, tc.WriteFile(filepath.Join(path, "fest.yaml"), "metadata:\n  id: "+id+"\n"))
	return path
}
