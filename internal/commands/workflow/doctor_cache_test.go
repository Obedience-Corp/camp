package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
)

func TestIsNavCacheStaleForWorkflow_FreshCacheNotStale(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow", "research"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "workflow", "research", "OBEY.md"),
		[]byte("hi\n"), 0o644))

	idx := &navindex.Index{BuildTime: time.Now().Add(time.Hour)}
	require.NoError(t, navindex.Save(idx, root))

	stale, err := isNavCacheStaleForWorkflow(context.Background(), root)
	require.NoError(t, err)
	assert.False(t, stale, "future-dated cache must not be flagged stale")
}

func TestIsNavCacheStaleForWorkflow_TouchedDeepFileTriggersStale(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "workflow", "research", "compare-llms")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(deep, ".workitem"), []byte("id: x\n"), 0o644))

	idx := &navindex.Index{BuildTime: time.Now().Add(-time.Hour)}
	require.NoError(t, navindex.Save(idx, root))

	touched := time.Now()
	require.NoError(t, os.Chtimes(filepath.Join(deep, ".workitem"), touched, touched))

	stale, err := isNavCacheStaleForWorkflow(context.Background(), root)
	require.NoError(t, err)
	assert.True(t, stale, "mtime on deep .workitem newer than cache build must mark stale")
}

func TestIsNavCacheStaleForWorkflow_NoWorkflowDirNotStale(t *testing.T) {
	root := t.TempDir()
	idx := &navindex.Index{BuildTime: time.Now()}
	require.NoError(t, navindex.Save(idx, root))

	stale, err := isNavCacheStaleForWorkflow(context.Background(), root)
	require.NoError(t, err)
	assert.False(t, stale, "missing workflow/ tree must not be flagged stale")
}
