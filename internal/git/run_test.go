package git_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

func TestRunGitCmd_Success(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	output, err := git.RunGitCmd(context.Background(), dir, "rev-parse", "--git-dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != ".git" {
		t.Errorf("expected '.git', got %q", output)
	}
}

func TestRunGitCmd_NotRepo(t *testing.T) {
	dir := t.TempDir()

	_, err := git.RunGitCmd(context.Background(), dir, "status")
	if err == nil {
		t.Fatal("expected error for non-repo directory")
	}
	if !errors.Is(err, camperrors.ErrNotInitialized) {
		t.Errorf("expected ErrNotInitialized (via ErrNotRepository), got: %v", err)
	}
}

func TestRunGitCmd_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := git.RunGitCmd(ctx, dir, "status")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestRunGitCmd_LockError(t *testing.T) {
	// We test the classification logic by creating a lock file and running
	// a command that would fail due to it. Create a repo, add a lock, try to commit.
	dir := t.TempDir()
	initRepo(t, dir)

	// Create index.lock
	lockPath := dir + "/.git/index.lock"
	if err := exec.Command("touch", lockPath).Run(); err != nil {
		t.Skipf("cannot create lock file: %v", err)
	}
	t.Cleanup(func() { exec.Command("rm", "-f", lockPath).Run() })

	// Try a commit which should fail with lock error
	_, err := git.RunGitCmd(context.Background(), dir, "commit", "--allow-empty", "-m", "test")
	if err == nil {
		t.Fatal("expected lock error")
	}

	var lockErr *git.LockError
	if !errors.As(err, &lockErr) {
		t.Errorf("expected *LockError, got: %T: %v", err, err)
	}
}

func TestHasStderr_CaseInsensitive(t *testing.T) {
	err := errors.New("fatal: No Such Remote 'ghost'")

	if !git.HasStderr(err, "no such remote") {
		t.Error("HasStderr should match case-insensitively")
	}
	if git.HasStderr(err, "already exists") {
		t.Error("HasStderr should not match unrelated substrings")
	}
	if git.HasStderr(nil, "anything") {
		t.Error("HasStderr(nil) should return false")
	}
}
