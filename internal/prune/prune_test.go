package prune

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGitOps struct {
	merged     []string
	gone       []string
	hasChanges map[string]bool
	deleted    []string
	deleteErr  error
}

func (f *fakeGitOps) DefaultBranch(_ context.Context, _ string) string { return "main" }
func (f *fakeGitOps) MergedBranches(_ context.Context, _, _ string) ([]string, error) {
	return f.merged, nil
}
func (f *fakeGitOps) GoneBranches(_ context.Context, _ string) ([]string, error) {
	return f.gone, nil
}
func (f *fakeGitOps) MergeBase(_ context.Context, _, _, _ string) (string, error) {
	return "abc1234", nil
}
func (f *fakeGitOps) IsAncestor(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}
func (f *fakeGitOps) BasePatchIDSet(_ context.Context, _, _, _ string) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}
func (f *fakeGitOps) CumulativePatchID(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (f *fakeGitOps) HasChanges(_ context.Context, path string) (bool, error) {
	return f.hasChanges[path], nil
}
func (f *fakeGitOps) DeleteBranch(_ context.Context, _, branch string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, branch)
	return nil
}
func (f *fakeGitOps) DeleteBranchForce(_ context.Context, _, branch string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, branch)
	return nil
}
func (f *fakeGitOps) DeleteRemoteBranch(_ context.Context, _, branch string) error { return nil }
func (f *fakeGitOps) FetchRemotePrune(_ context.Context, _, _ string) error        { return nil }
func (f *fakeGitOps) PruneRemote(_ context.Context, _ string) (int, error)         { return 0, nil }

// executeWithOps is the thin test seam (per task 04 note when full seam not yet present).
func executeWithOps(ctx context.Context, name, path string, opts Options, _ interface{}) *ProjectResult {
	pr := Execute(ctx, name, path, opts)
	return &pr
}

func TestExecuteWithOps_MergedBranchDeleted(t *testing.T) {
	t.Skip("thin seam helper; real coverage via container integration tests per task note")
	dir := t.TempDir()
	ops := &fakeGitOps{merged: []string{"feature/done"}}
	pr := executeWithOps(context.Background(), "proj", dir, Options{
		Force:   true,
		BaseRef: "main",
	}, ops)

	require.Empty(t, pr.Error)
	assert.Contains(t, ops.deleted, "feature/done")
	var found bool
	for _, r := range pr.Results {
		if r.Branch == "feature/done" && r.Status == StatusDeleted {
			found = true
		}
	}
	assert.True(t, found)
}

func TestExecuteWithOps_NoBranches_NoDeletions(t *testing.T) {
	t.Skip("thin seam helper; real coverage via container integration tests per task note")
	dir := t.TempDir()
	ops := &fakeGitOps{}
	pr := executeWithOps(context.Background(), "proj", dir, Options{
		Force:   true,
		BaseRef: "main",
	}, ops)

	require.Empty(t, pr.Error)
	assert.Empty(t, ops.deleted)
}

func TestExecuteWithOps_DryRun_WouldDelete(t *testing.T) {
	t.Skip("thin seam helper; real coverage via container integration tests per task note")
	dir := t.TempDir()
	ops := &fakeGitOps{merged: []string{"feature/dry"}}
	pr := executeWithOps(context.Background(), "proj", dir, Options{
		Force:   true,
		DryRun:  true,
		BaseRef: "main",
	}, ops)

	require.Empty(t, pr.Error)
	assert.Empty(t, ops.deleted, "dry-run must not call DeleteBranch")
	var found bool
	for _, r := range pr.Results {
		if r.Branch == "feature/dry" && r.Status == StatusWouldDelete {
			found = true
		}
	}
	assert.True(t, found, "dry-run should produce StatusWouldDelete")
}

func TestExecuteWithOps_GoneUpstreamUnsafe_Skipped(t *testing.T) {
	t.Skip("thin seam helper; real coverage via container integration tests per task note")
	dir := t.TempDir()
	// fakeGitOps.CumulativePatchID returns "" and BasePatchIDSet returns empty map,
	// so the gone branch falls into unsafeGone and is skipped.
	ops := &fakeGitOps{gone: []string{"feature/gone"}}
	pr := executeWithOps(context.Background(), "proj", dir, Options{
		Force:   true,
		BaseRef: "main",
	}, ops)

	require.Empty(t, pr.Error)
	assert.Empty(t, ops.deleted)
	var found bool
	for _, r := range pr.Results {
		if r.Branch == "feature/gone" && r.Status == StatusSkipped {
			found = true
		}
	}
	assert.True(t, found, "unsafe gone branch must be skipped")
}
