//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCp_SameFileGuard(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/cp-samefile", "cp-samefile", "product")
	require.NoError(t, err)

	// Create a file with known content.
	tc.Shell(t, `
set -e
mkdir -p /campaigns/cp-samefile/docs
printf 'important content\n' > /campaigns/cp-samefile/docs/notes.md
`)

	// Try to copy the file into its own directory (same-file truncation scenario).
	_, err = tc.RunCampInDir("/campaigns/cp-samefile/docs",
		"cp", "--force", "notes.md", ".")
	assert.Error(t, err, "same-file copy should fail")
	assert.Contains(t, err.Error(), "same file", "error should mention same file")

	// Verify the original file content is unchanged.
	content, err := tc.ReadFile("/campaigns/cp-samefile/docs/notes.md")
	require.NoError(t, err)
	assert.Contains(t, content, "important content", "file content must not be truncated")
}

func TestCp_SelfRecursionGuard(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/cp-recurse", "cp-recurse", "product")
	require.NoError(t, err)

	tc.Shell(t, `
set -e
mkdir -p /campaigns/cp-recurse/mydir
printf 'file1\n' > /campaigns/cp-recurse/mydir/a.txt
printf 'file2\n' > /campaigns/cp-recurse/mydir/b.txt
`)

	// Try to copy a directory into itself.
	_, err = tc.RunCampInDir("/campaigns/cp-recurse",
		"cp", "--force", "mydir", "mydir/subdir")
	assert.Error(t, err, "copying a directory into itself should fail")
	assert.Contains(t, err.Error(), "into itself", "error should mention self-copy")

	// Original files must still exist.
	exists, err := tc.CheckFileExists("/campaigns/cp-recurse/mydir/a.txt")
	require.NoError(t, err)
	assert.True(t, exists, "original files must not be affected")
}

func TestCp_NormalFileCopyWorks(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/cp-normal", "cp-normal", "product")
	require.NoError(t, err)

	tc.Shell(t, `
set -e
mkdir -p /campaigns/cp-normal/src /campaigns/cp-normal/dest
printf 'hello world\n' > /campaigns/cp-normal/src/file.md
`)

	_, err = tc.RunCampInDir("/campaigns/cp-normal",
		"cp", "src/file.md", "dest/")
	require.NoError(t, err, "normal file copy should succeed")

	content, err := tc.ReadFile("/campaigns/cp-normal/dest/file.md")
	require.NoError(t, err)
	assert.Contains(t, content, "hello world")
}

func TestCp_NormalDirCopyWorks(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/cp-dir", "cp-dir", "product")
	require.NoError(t, err)

	tc.Shell(t, `
set -e
mkdir -p /campaigns/cp-dir/srcdir
printf 'alpha\n' > /campaigns/cp-dir/srcdir/alpha.md
printf 'beta\n'  > /campaigns/cp-dir/srcdir/beta.md
mkdir -p /campaigns/cp-dir/destparent
`)

	_, err = tc.RunCampInDir("/campaigns/cp-dir",
		"cp", "srcdir", "destparent/")
	require.NoError(t, err, "normal directory copy should succeed")

	content, err := tc.ReadFile("/campaigns/cp-dir/destparent/srcdir/alpha.md")
	require.NoError(t, err)
	assert.Contains(t, content, "alpha")
}
