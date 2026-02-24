package commitkit_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/pkg/commitkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// FormatCampaignTag
// ---------------------------------------------------------------------------

func TestFormatCampaignTag(t *testing.T) {
	tests := []struct {
		name       string
		campaignID string
		want       string
	}{
		{
			name:       "empty ID returns empty string",
			campaignID: "",
			want:       "",
		},
		{
			name:       "short ID returned verbatim inside tag",
			campaignID: "abc123",
			want:       "[OBEY-CAMPAIGN-abc123]",
		},
		{
			name:       "exactly 8 chars not truncated",
			campaignID: "12345678",
			want:       "[OBEY-CAMPAIGN-12345678]",
		},
		{
			name:       "ID longer than 8 chars is truncated to 8",
			campaignID: "abcdef1234567890",
			want:       "[OBEY-CAMPAIGN-abcdef12]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := commitkit.FormatCampaignTag(tc.campaignID)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// PrependCampaignTag
// ---------------------------------------------------------------------------

func TestPrependCampaignTag(t *testing.T) {
	tests := []struct {
		name       string
		campaignID string
		message    string
		want       string
	}{
		{
			name:       "empty campaignID returns message unchanged",
			campaignID: "",
			message:    "fix: something",
			want:       "fix: something",
		},
		{
			name:       "non-empty ID prepends tag with space",
			campaignID: "abc123",
			message:    "feat: add thing",
			want:       "[OBEY-CAMPAIGN-abc123] feat: add thing",
		},
		{
			name:       "long ID is truncated before prepend",
			campaignID: "abcdef1234567890",
			message:    "chore: update",
			want:       "[OBEY-CAMPAIGN-abcdef12] chore: update",
		},
		{
			name:       "empty message with valid ID",
			campaignID: "abc123",
			message:    "",
			want:       "[OBEY-CAMPAIGN-abc123] ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := commitkit.PrependCampaignTag(tc.campaignID, tc.message)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers for creating a real campaign on disk
// ---------------------------------------------------------------------------

// makeCampaign creates a temporary directory with a minimal .campaign/campaign.yaml.
// Returns the campaign root and a cleanup function.
func makeCampaign(t *testing.T, id string) string {
	t.Helper()

	root := t.TempDir()
	campaignDir := filepath.Join(root, ".campaign")
	require.NoError(t, os.MkdirAll(campaignDir, 0755))

	yaml := "id: " + id + "\nname: test-campaign\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(campaignDir, "campaign.yaml"),
		[]byte(yaml),
		0644,
	))

	return root
}

// ---------------------------------------------------------------------------
// LoadCampaignID
// ---------------------------------------------------------------------------

func TestLoadCampaignID(t *testing.T) {
	t.Run("returns ID from valid campaign", func(t *testing.T) {
		root := makeCampaign(t, "test-id-1234")
		id, err := commitkit.LoadCampaignID(context.Background(), root)
		require.NoError(t, err)
		assert.Equal(t, "test-id-1234", id)
	})

	t.Run("returns error when campaign.yaml missing", func(t *testing.T) {
		root := t.TempDir() // no .campaign/ directory
		_, err := commitkit.LoadCampaignID(context.Background(), root)
		require.Error(t, err)
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		root := makeCampaign(t, "test-id-cancel")
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately
		_, err := commitkit.LoadCampaignID(ctx, root)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// ---------------------------------------------------------------------------
// DetectCampaign
// ---------------------------------------------------------------------------

func TestDetectCampaign(t *testing.T) {
	t.Run("detects campaign from within campaign root", func(t *testing.T) {
		root := makeCampaign(t, "detect-id-5678")

		// Change working directory to the campaign root for this test.
		origWd, err := os.Getwd()
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Chdir(origWd) })
		require.NoError(t, os.Chdir(root))

		id, err := commitkit.DetectCampaign(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "detect-id-5678", id)
	})

	t.Run("detects campaign from subdirectory", func(t *testing.T) {
		root := makeCampaign(t, "detect-sub-id")
		sub := filepath.Join(root, "projects", "mypkg")
		require.NoError(t, os.MkdirAll(sub, 0755))

		origWd, err := os.Getwd()
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Chdir(origWd) })
		require.NoError(t, os.Chdir(sub))

		id, err := commitkit.DetectCampaign(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "detect-sub-id", id)
	})

	t.Run("returns error when not inside a campaign", func(t *testing.T) {
		// Use a bare temp dir with no .campaign/ anywhere above it.
		bare := t.TempDir()

		origWd, err := os.Getwd()
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Chdir(origWd) })
		require.NoError(t, os.Chdir(bare))

		_, err = commitkit.DetectCampaign(context.Background())
		require.Error(t, err)
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		root := makeCampaign(t, "detect-cancel-id")

		origWd, err := os.Getwd()
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Chdir(origWd) })
		require.NoError(t, os.Chdir(root))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = commitkit.DetectCampaign(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// ---------------------------------------------------------------------------
// Helpers for SyncSubmoduleRef
// ---------------------------------------------------------------------------

// makeGitRepo initialises a bare git repo at dir with an initial commit so
// the repo has a valid HEAD.
func makeGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@example.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "config", "protocol.file.allow", "always"},
	}
	for _, args := range cmds {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		require.NoError(t, err, "git setup: %s", string(out))
	}

	// Create an initial commit so HEAD exists.
	readme := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("# test\n"), 0644))
	out, err := exec.Command("git", "-C", dir, "add", ".").CombinedOutput()
	require.NoError(t, err, "%s", string(out))
	out, err = exec.Command("git", "-C", dir, "commit", "-m", "init").CombinedOutput()
	require.NoError(t, err, "%s", string(out))
}

// addSubmodule adds sub as a submodule of parent at the given relPath.
func addSubmodule(t *testing.T, parent, sub, relPath string) {
	t.Helper()
	out, err := exec.Command("git", "-c", "protocol.file.allow=always",
		"-C", parent, "submodule", "add", sub, relPath).CombinedOutput()
	require.NoError(t, err, "add submodule: %s", string(out))
	out, err = exec.Command("git", "-C", parent, "commit", "-m", "add submodule").CombinedOutput()
	require.NoError(t, err, "commit submodule: %s", string(out))
}

// ---------------------------------------------------------------------------
// SyncSubmoduleRef
// ---------------------------------------------------------------------------

func TestSyncSubmoduleRef(t *testing.T) {
	t.Run("no-op when submodule pointer unchanged", func(t *testing.T) {
		parent := t.TempDir()
		sub := t.TempDir()
		makeGitRepo(t, parent)
		makeGitRepo(t, sub)
		addSubmodule(t, parent, sub, "sub")

		// Nothing has changed in sub — syncing should be a no-op.
		err := commitkit.SyncSubmoduleRef(context.Background(), parent, "sub", "testid")
		require.NoError(t, err)
	})

	t.Run("commits updated submodule pointer", func(t *testing.T) {
		parent := t.TempDir()
		sub := t.TempDir()
		makeGitRepo(t, parent)
		makeGitRepo(t, sub)
		addSubmodule(t, parent, sub, "sub")

		// Advance the submodule HEAD so the pointer in parent is stale.
		newFile := filepath.Join(sub, "new.txt")
		require.NoError(t, os.WriteFile(newFile, []byte("hello\n"), 0644))
		out, err := exec.Command("git", "-C", sub, "add", ".").CombinedOutput()
		require.NoError(t, err, "%s", string(out))
		out, err = exec.Command("git", "-C", sub, "commit", "-m", "advance").CombinedOutput()
		require.NoError(t, err, "%s", string(out))

		// Update the submodule checkout in parent to the new commit.
		out, err = exec.Command("git", "-c", "protocol.file.allow=always",
			"-C", parent, "submodule", "update", "--remote", "sub").CombinedOutput()
		require.NoError(t, err, "%s", string(out))

		// Now the pointer is dirty in parent — SyncSubmoduleRef should commit it.
		err = commitkit.SyncSubmoduleRef(context.Background(), parent, "sub", "abc12345")
		require.NoError(t, err)

		// Verify a new commit exists with the expected campaign-tagged message.
		out, err = exec.Command("git", "-C", parent, "log", "--oneline", "-1").CombinedOutput()
		require.NoError(t, err)
		msg := string(out)
		assert.Contains(t, msg, "[OBEY-CAMPAIGN-abc12345]")
		assert.Contains(t, msg, "sync submodule ref: sub")
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		parent := t.TempDir()
		makeGitRepo(t, parent)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := commitkit.SyncSubmoduleRef(ctx, parent, "sub", "testid")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// ---------------------------------------------------------------------------
// StageAll
// ---------------------------------------------------------------------------

func TestStageAll(t *testing.T) {
	t.Run("stages new and modified files", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		// Create a new file.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0644))

		err := commitkit.StageAll(context.Background(), dir)
		require.NoError(t, err)

		has, err := commitkit.HasStagedChanges(context.Background(), dir)
		require.NoError(t, err)
		assert.True(t, has)
	})

	t.Run("stages deletions", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		// Delete the README that was committed in makeGitRepo.
		require.NoError(t, os.Remove(filepath.Join(dir, "README.md")))

		err := commitkit.StageAll(context.Background(), dir)
		require.NoError(t, err)

		has, err := commitkit.HasStagedChanges(context.Background(), dir)
		require.NoError(t, err)
		assert.True(t, has)
	})

	t.Run("no-op on clean repo", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		err := commitkit.StageAll(context.Background(), dir)
		require.NoError(t, err)

		has, err := commitkit.HasStagedChanges(context.Background(), dir)
		require.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := commitkit.StageAll(ctx, dir)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("succeeds with stale lock file", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("data\n"), 0644))

		// Create a stale lock (no process holding it).
		lockPath := filepath.Join(dir, ".git", "index.lock")
		require.NoError(t, os.WriteFile(lockPath, []byte{}, 0644))

		err := commitkit.StageAll(context.Background(), dir)
		require.NoError(t, err)

		// Lock should have been cleaned up.
		_, err = os.Stat(lockPath)
		assert.True(t, os.IsNotExist(err), "stale lock should be removed")
	})
}

// ---------------------------------------------------------------------------
// StageFiles
// ---------------------------------------------------------------------------

func TestStageFiles(t *testing.T) {
	t.Run("stages specific file only", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0644))

		err := commitkit.StageFiles(context.Background(), dir, "a.txt")
		require.NoError(t, err)

		// Verify only a.txt is staged by checking diff output.
		out, err := exec.Command("git", "-C", dir, "diff", "--cached", "--name-only").Output()
		require.NoError(t, err)
		assert.Contains(t, string(out), "a.txt")
		assert.NotContains(t, string(out), "b.txt")
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := commitkit.StageFiles(ctx, dir, "file.txt")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// ---------------------------------------------------------------------------
// HasStagedChanges
// ---------------------------------------------------------------------------

func TestHasStagedChanges(t *testing.T) {
	t.Run("returns false on clean repo", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		has, err := commitkit.HasStagedChanges(context.Background(), dir)
		require.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns true with staged changes", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0644))
		_, err := exec.Command("git", "-C", dir, "add", "new.txt").CombinedOutput()
		require.NoError(t, err)

		has, err := commitkit.HasStagedChanges(context.Background(), dir)
		require.NoError(t, err)
		assert.True(t, has)
	})

	t.Run("returns false with only unstaged changes", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		// Modify existing file without staging.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("modified\n"), 0644))

		has, err := commitkit.HasStagedChanges(context.Background(), dir)
		require.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := commitkit.HasStagedChanges(ctx, dir)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// ---------------------------------------------------------------------------
// Commit
// ---------------------------------------------------------------------------

func TestCommit(t *testing.T) {
	t.Run("commits staged changes", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0644))
		require.NoError(t, commitkit.StageAll(context.Background(), dir))

		err := commitkit.Commit(context.Background(), dir, commitkit.CommitOptions{
			Message: "test commit",
		})
		require.NoError(t, err)

		// Verify the commit message.
		out, err := exec.Command("git", "-C", dir, "log", "--oneline", "-1").Output()
		require.NoError(t, err)
		assert.Contains(t, string(out), "test commit")
	})

	t.Run("returns ErrNoChanges when nothing staged", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		err := commitkit.Commit(context.Background(), dir, commitkit.CommitOptions{
			Message: "empty",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, commitkit.ErrNoChanges))
	})

	t.Run("succeeds with stale lock file", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data\n"), 0644))
		require.NoError(t, commitkit.StageAll(context.Background(), dir))

		// Create a stale lock (no process holding it).
		lockPath := filepath.Join(dir, ".git", "index.lock")
		require.NoError(t, os.WriteFile(lockPath, []byte{}, 0644))

		err := commitkit.Commit(context.Background(), dir, commitkit.CommitOptions{
			Message: "commit with lock",
		})
		require.NoError(t, err)

		// Lock should have been cleaned up.
		_, err = os.Stat(lockPath)
		assert.True(t, os.IsNotExist(err), "stale lock should be removed")
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := commitkit.Commit(ctx, dir, commitkit.CommitOptions{Message: "msg"})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// ---------------------------------------------------------------------------
// CommitAll
// ---------------------------------------------------------------------------

func TestCommitAll(t *testing.T) {
	t.Run("stages and commits all changes", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb\n"), 0644))

		err := commitkit.CommitAll(context.Background(), dir, "commit all test")
		require.NoError(t, err)

		// Verify the commit.
		out, err := exec.Command("git", "-C", dir, "log", "--oneline", "-1").Output()
		require.NoError(t, err)
		assert.Contains(t, string(out), "commit all test")

		// Verify clean working tree.
		has, err := commitkit.HasStagedChanges(context.Background(), dir)
		require.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns ErrNoChanges on clean repo", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		err := commitkit.CommitAll(context.Background(), dir, "nothing here")
		require.Error(t, err)
		assert.True(t, errors.Is(err, commitkit.ErrNoChanges))
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := commitkit.CommitAll(ctx, dir, "msg")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// ---------------------------------------------------------------------------
// ShortHash
// ---------------------------------------------------------------------------

func TestShortHash(t *testing.T) {
	t.Run("returns non-empty hash", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		hash, err := commitkit.ShortHash(context.Background(), dir)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		// Short hashes are typically 7 chars.
		assert.GreaterOrEqual(t, len(hash), 7)
	})

	t.Run("hash changes after new commit", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		hash1, err := commitkit.ShortHash(context.Background(), dir)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("data\n"), 0644))
		require.NoError(t, commitkit.CommitAll(context.Background(), dir, "second commit"))

		hash2, err := commitkit.ShortHash(context.Background(), dir)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		dir := t.TempDir()
		makeGitRepo(t, dir)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := commitkit.ShortHash(ctx, dir)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// ---------------------------------------------------------------------------
// ErrNoChanges sentinel
// ---------------------------------------------------------------------------

func TestErrNoChanges(t *testing.T) {
	t.Run("is compatible with errors.Is", func(t *testing.T) {
		assert.True(t, errors.Is(commitkit.ErrNoChanges, commitkit.ErrNoChanges))
	})
}
