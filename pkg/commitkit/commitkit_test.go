package commitkit_test

import (
	"context"
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
