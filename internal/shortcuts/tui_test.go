package shortcuts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListDirsUnder(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir := t.TempDir()

	// Create test directory structure:
	// tmpDir/
	//   dir1/
	//     subdir1/
	//       deep/
	//         tooDeep/
	//     subdir2/
	//   dir2/
	//   .hidden/
	//   file.txt
	dirs := []string{
		"dir1",
		"dir1/subdir1",
		"dir1/subdir1/deep",
		"dir1/subdir1/deep/tooDeep",
		"dir1/subdir2",
		"dir2",
		".hidden",
	}
	for _, d := range dirs {
		err := os.MkdirAll(filepath.Join(tmpDir, d), 0755)
		require.NoError(t, err)
	}

	// Create a file (should be ignored)
	err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("test"), 0644)
	require.NoError(t, err)

	t.Run("maxDepth 1", func(t *testing.T) {
		result := listDirsUnder(tmpDir, 1)
		assert.Contains(t, result, "dir1/")
		assert.Contains(t, result, "dir2/")
		assert.NotContains(t, result, "dir1/subdir1/")
		assert.NotContains(t, result, ".hidden/")
		assert.NotContains(t, result, "file.txt")
	})

	t.Run("maxDepth 2", func(t *testing.T) {
		result := listDirsUnder(tmpDir, 2)
		assert.Contains(t, result, "dir1/")
		assert.Contains(t, result, "dir2/")
		assert.Contains(t, result, "dir1/subdir1/")
		assert.Contains(t, result, "dir1/subdir2/")
		assert.NotContains(t, result, "dir1/subdir1/deep/")
	})

	t.Run("maxDepth 3", func(t *testing.T) {
		result := listDirsUnder(tmpDir, 3)
		assert.Contains(t, result, "dir1/")
		assert.Contains(t, result, "dir1/subdir1/")
		assert.Contains(t, result, "dir1/subdir1/deep/")
		assert.NotContains(t, result, "dir1/subdir1/deep/tooDeep/")
	})

	t.Run("hidden dirs excluded", func(t *testing.T) {
		result := listDirsUnder(tmpDir, 5)
		assert.NotContains(t, result, ".hidden/")
	})

	t.Run("non-existent directory", func(t *testing.T) {
		result := listDirsUnder(filepath.Join(tmpDir, "nonexistent"), 2)
		assert.Empty(t, result)
	})

	t.Run("results are sorted", func(t *testing.T) {
		result := listDirsUnder(tmpDir, 2)
		require.True(t, len(result) > 1)
		for i := 1; i < len(result); i++ {
			assert.True(t, result[i-1] <= result[i], "results should be sorted: %s > %s", result[i-1], result[i])
		}
	})
}

func TestAddSubShortcutResult(t *testing.T) {
	result := &AddSubShortcutResult{
		ProjectName:  "my-project",
		ShortcutName: "cli",
		ShortcutPath: "cmd/app/",
	}

	assert.Equal(t, "my-project", result.ProjectName)
	assert.Equal(t, "cli", result.ShortcutName)
	assert.Equal(t, "cmd/app/", result.ShortcutPath)
}

func TestAddJumpResult(t *testing.T) {
	result := &AddJumpResult{
		Name:        "api",
		Path:        "projects/api-service/",
		Description: "Jump to API service",
		Concept:     "project",
	}

	assert.Equal(t, "api", result.Name)
	assert.Equal(t, "projects/api-service/", result.Path)
	assert.Equal(t, "Jump to API service", result.Description)
	assert.Equal(t, "project", result.Concept)
}
