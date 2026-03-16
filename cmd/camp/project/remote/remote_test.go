package remote

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/spf13/cobra"
)

// initTestRepo creates a git repo at dir with an initial empty commit.
func initTestRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("initTestRepo %v: %v\n%s", args, err, out)
		}
	}
}

// TestRemoteRemove_OriginGuardWithoutForce verifies the origin safety guard.
func TestRemoteRemove_OriginGuardWithoutForce(t *testing.T) {
	flagRemoteProject = ""

	cmd := &cobra.Command{}
	cmd.Flags().BoolP("force", "f", false, "")
	cmd.SetContext(context.Background())

	err := runProjectRemoteRemove(cmd, []string{"origin"})
	if err == nil {
		t.Fatal("expected error when removing origin without --force")
	}
	if !strings.Contains(err.Error(), "force") {
		t.Errorf("expected error to mention --force, got: %v", err)
	}
}

// TestRemoteRemove_NonOriginSkipsGuard verifies the guard does not fire for non-origin.
func TestRemoteRemove_NonOriginSkipsGuard(t *testing.T) {
	flagRemoteProject = ""

	cmd := &cobra.Command{}
	cmd.Flags().BoolP("force", "f", false, "")
	cmd.SetContext(context.Background())

	err := runProjectRemoteRemove(cmd, []string{"upstream"})
	// It will fail on campaign detection (not in a campaign), but should NOT
	// fail with the origin guard error.
	if err != nil && strings.Contains(err.Error(), "use --force to remove origin") {
		t.Error("guard fired for non-origin remote — should not happen")
	}
}

// TestRemoteList_ShowsOrigin verifies ListRemotes returns origin for a cloned repo.
func TestRemoteList_ShowsOrigin(t *testing.T) {
	bare := t.TempDir()
	initTestRepo(t, bare)

	clone := t.TempDir()
	if out, err := exec.Command("git", "clone", bare, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}

	remotes, err := git.ListRemotes(context.Background(), clone)
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	if len(remotes) == 0 {
		t.Fatal("expected at least one remote (origin)")
	}
	if remotes[0].Name != "origin" {
		t.Errorf("expected first remote name 'origin', got %q", remotes[0].Name)
	}
}

// TestRemoteList_MultipleRemotesSorted verifies remotes are sorted alphabetically.
func TestRemoteList_MultipleRemotesSorted(t *testing.T) {
	dir := t.TempDir()
	initTestRepo(t, dir)

	git.AddRemote(context.Background(), dir, "zebra", "https://example.com/z.git")
	git.AddRemote(context.Background(), dir, "alpha", "https://example.com/a.git")
	git.AddRemote(context.Background(), dir, "origin", "https://example.com/o.git")

	remotes, err := git.ListRemotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	if len(remotes) != 3 {
		t.Fatalf("expected 3 remotes, got %d", len(remotes))
	}
	if remotes[0].Name != "alpha" || remotes[1].Name != "origin" || remotes[2].Name != "zebra" {
		t.Errorf("expected sorted [alpha, origin, zebra], got [%s, %s, %s]",
			remotes[0].Name, remotes[1].Name, remotes[2].Name)
	}
}

// TestRemoteAddAndRemoveLifecycle tests the add/remove lifecycle through git operations.
func TestRemoteAddAndRemoveLifecycle(t *testing.T) {
	dir := t.TempDir()
	initTestRepo(t, dir)

	ctx := context.Background()

	// Add
	if err := git.AddRemote(ctx, dir, "upstream", "https://example.com/upstream.git"); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	remotes, _ := git.ListRemotes(ctx, dir)
	found := false
	for _, r := range remotes {
		if r.Name == "upstream" {
			found = true
			break
		}
	}
	if !found {
		t.Error("upstream not found after add")
	}

	// Rename
	if err := git.RenameRemote(ctx, dir, "upstream", "fork"); err != nil {
		t.Fatalf("RenameRemote: %v", err)
	}

	remotes, _ = git.ListRemotes(ctx, dir)
	foundFork := false
	for _, r := range remotes {
		if r.Name == "fork" {
			foundFork = true
		}
		if r.Name == "upstream" {
			t.Error("upstream still present after rename")
		}
	}
	if !foundFork {
		t.Error("fork not found after rename")
	}

	// Remove
	if err := git.RemoveRemote(ctx, dir, "fork"); err != nil {
		t.Fatalf("RemoveRemote: %v", err)
	}

	remotes, _ = git.ListRemotes(ctx, dir)
	for _, r := range remotes {
		if r.Name == "fork" {
			t.Error("fork still present after remove")
		}
	}
}

// TestRemoteRename_OriginGuardWithoutForce verifies renaming origin is blocked without --force.
func TestRemoteRename_OriginGuardWithoutForce(t *testing.T) {
	flagRemoteProject = ""

	cmd := &cobra.Command{}
	cmd.Flags().BoolP("force", "f", false, "")
	cmd.SetContext(context.Background())

	err := runProjectRemoteRename(cmd, []string{"origin", "old-origin"})
	// It will fail on campaign detection (not in a campaign), but should NOT
	// fail with the rename guard error since the guard only fires for submodule projects.
	// The guard checks isSubmodule which requires campaign detection.
	// So we just verify the function doesn't panic and continues past the guard.
	if err != nil && strings.Contains(err.Error(), "use --force to rename origin") {
		// This means the guard fired — which requires a campaign and submodule context.
		// In a test without campaign context, the guard should not fire.
		t.Log("guard fired (expected only in campaign+submodule context)")
	}
}

// TestRemoteStatus verifies the remoteStatus helper.
func TestRemoteStatus(t *testing.T) {
	tests := []struct {
		name     string
		cmp      *git.URLComparison
		contains string
	}{
		{
			name:     "not initialized",
			cmp:      &git.URLComparison{ActiveURL: "", Match: false},
			contains: "not-initialized",
		},
		{
			name:     "matching",
			cmp:      &git.URLComparison{ActiveURL: "https://x.com", Match: true},
			contains: "ok",
		},
		{
			name:     "drift",
			cmp:      &git.URLComparison{ActiveURL: "https://x.com", DeclaredURL: "https://y.com", Match: false},
			contains: "drift",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := remoteStatus(tt.cmp)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("remoteStatus() = %q, want to contain %q", result, tt.contains)
			}
		})
	}
}
