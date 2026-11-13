//go:build container_fs

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stageScopeCamps(t *testing.T) (root, other string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	root = filepath.Join(base, "camp-a")
	other = filepath.Join(base, "camp-b")
	for _, d := range []string{
		filepath.Join(root, "projects", "app"),
		filepath.Join(root, "projects", "worktrees", "app", "feature"),
		other,
	} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	return root, other
}

func TestNavScopeToggleFromOutsideRootIsNotStored(t *testing.T) {
	root, other := stageScopeCamps(t)

	require.NoError(t, RecordJump(t.Context(), root, other, root))

	entries, err := LoadHistory(t.Context(), root)
	require.NoError(t, err)
	assert.Empty(t, entries)
	last, err := GetLastLocation(t.Context(), root)
	require.NoError(t, err)
	assert.Empty(t, last)
}

func TestNavScopeSetLastLocationOutsideRootIsNotStored(t *testing.T) {
	root, other := stageScopeCamps(t)

	require.NoError(t, SetLastLocation(t.Context(), root, other))
	require.NoError(t, RecordVisit(t.Context(), root, other))

	entries, err := LoadHistory(t.Context(), root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestNavScopePollutedHistoryFallsBackToInRootEntry(t *testing.T) {
	root, other := stageScopeCamps(t)
	app := filepath.Join(root, "projects", "app")
	require.NoError(t, SetLastLocation(t.Context(), root, app))

	polluted := `{"location":"` + other + `","rel":"../camp-b","ts":"2026-11-13T02:00:00Z","kind":"toggle"}` + "\n"
	f, err := os.OpenFile(StatePath(root), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(polluted)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	last, err := GetLastLocation(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, app, last)
}

func TestNavScopeOnlyPollutedHistoryYieldsNothing(t *testing.T) {
	root, other := stageScopeCamps(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(StatePath(root)), 0o755))
	polluted := `{"location":"` + other + `","rel":"../camp-b","ts":"2026-11-13T02:00:00Z","kind":"toggle"}` + "\n"
	require.NoError(t, os.WriteFile(StatePath(root), []byte(polluted), 0o600))

	last, err := GetLastLocation(t.Context(), root)
	require.NoError(t, err)
	assert.Empty(t, last)
}

func TestNavScopeInRootTogglingStillAlternates(t *testing.T) {
	root, _ := stageScopeCamps(t)
	wt := filepath.Join(root, "projects", "worktrees", "app", "feature")

	require.NoError(t, RecordJump(t.Context(), root, wt, root))
	last, err := GetLastLocation(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, wt, last)

	require.NoError(t, RecordJump(t.Context(), root, root, wt))
	last, err = GetLastLocation(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, root, last)
}

func TestNavScopeLinkedProjectCountsAsInside(t *testing.T) {
	root, other := stageScopeCamps(t)
	link := filepath.Join(root, "projects", "linked")
	require.NoError(t, os.Symlink(other, link))

	assert.True(t, InsideRoot(root, link))
	require.NoError(t, RecordJump(t.Context(), root, link, root))
	last, err := GetLastLocation(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, link, last)
}

func TestNavScopeSymlinkedRootMatches(t *testing.T) {
	root, _ := stageScopeCamps(t)
	alias := filepath.Join(filepath.Dir(root), "alias")
	require.NoError(t, os.Symlink(root, alias))

	assert.True(t, InsideRoot(alias, filepath.Join(root, "projects", "app")))
	assert.True(t, InsideRoot(root, filepath.Join(alias, "projects", "app")))
	assert.False(t, InsideRoot(alias, filepath.Join(filepath.Dir(root), "camp-b")))
}
