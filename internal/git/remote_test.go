package git_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// initRepo creates a git repo at dir with an initial empty commit so that
// submodule and ls-remote operations work correctly.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("initRepo %v: %v\n%s", args, err, out)
		}
	}
}

func TestListRemotes_NoRemotes(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	remotes, err := git.ListRemotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remotes) != 0 {
		t.Fatalf("expected 0 remotes, got %d", len(remotes))
	}
}

func TestListRemotes_SingleRemote(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	exec.Command("git", "-C", dir, "remote", "add", "origin", "https://example.com/repo.git").Run()

	remotes, err := git.ListRemotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remotes) != 1 {
		t.Fatalf("expected 1 remote, got %d", len(remotes))
	}
	if remotes[0].Name != "origin" {
		t.Errorf("expected name 'origin', got %q", remotes[0].Name)
	}
	if remotes[0].FetchURL != "https://example.com/repo.git" {
		t.Errorf("unexpected FetchURL: %q", remotes[0].FetchURL)
	}
}

func TestListRemotes_SortedOrder(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	exec.Command("git", "-C", dir, "remote", "add", "zebra", "https://example.com/z.git").Run()
	exec.Command("git", "-C", dir, "remote", "add", "alpha", "https://example.com/a.git").Run()

	remotes, err := git.ListRemotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remotes) != 2 {
		t.Fatalf("expected 2 remotes, got %d", len(remotes))
	}
	if remotes[0].Name != "alpha" || remotes[1].Name != "zebra" {
		t.Errorf("expected sorted order [alpha, zebra], got [%s, %s]",
			remotes[0].Name, remotes[1].Name)
	}
}

func TestAddRemote_Success(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	err := git.AddRemote(context.Background(), dir, "origin", "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	remotes, _ := git.ListRemotes(context.Background(), dir)
	if len(remotes) != 1 || remotes[0].Name != "origin" {
		t.Error("remote not found after add")
	}
}

func TestAddRemote_Duplicate(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	git.AddRemote(context.Background(), dir, "origin", "https://example.com/repo.git")

	err := git.AddRemote(context.Background(), dir, "origin", "https://other.com/repo.git")
	if err == nil {
		t.Fatal("expected error for duplicate remote, got nil")
	}
	if !errors.Is(err, camperrors.ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got: %v", err)
	}
}

func TestRemoveRemote_Success(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	git.AddRemote(context.Background(), dir, "origin", "https://example.com/repo.git")

	err := git.RemoveRemote(context.Background(), dir, "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	remotes, _ := git.ListRemotes(context.Background(), dir)
	if len(remotes) != 0 {
		t.Error("remote still present after remove")
	}
}

func TestRemoveRemote_NotFound(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	err := git.RemoveRemote(context.Background(), dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, camperrors.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestRenameRemote_Success(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	git.AddRemote(context.Background(), dir, "origin", "https://example.com/repo.git")

	err := git.RenameRemote(context.Background(), dir, "origin", "upstream")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	remotes, _ := git.ListRemotes(context.Background(), dir)
	if len(remotes) != 1 || remotes[0].Name != "upstream" {
		t.Error("rename did not take effect")
	}
}

func TestRenameRemote_NotFound(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	err := git.RenameRemote(context.Background(), dir, "ghost", "upstream")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, camperrors.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestSetRemoteURL_Success(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	git.AddRemote(context.Background(), dir, "origin", "https://example.com/old.git")

	err := git.SetRemoteURL(context.Background(), dir, "origin", "https://example.com/new.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	remotes, _ := git.ListRemotes(context.Background(), dir)
	if len(remotes) != 1 || remotes[0].FetchURL != "https://example.com/new.git" {
		t.Errorf("URL not updated: %+v", remotes)
	}
}

func TestSetRemoteURL_NotFound(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	err := git.SetRemoteURL(context.Background(), dir, "ghost", "https://example.com/new.git")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, camperrors.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestVerifyRemote_LocalSuccess(t *testing.T) {
	remote := t.TempDir()
	initRepo(t, remote)

	local := t.TempDir()
	initRepo(t, local)
	git.AddRemote(context.Background(), local, "origin", remote)

	err := git.VerifyRemote(context.Background(), local, "origin")
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestVerifyRemote_NotFound(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	err := git.VerifyRemote(context.Background(), dir, "ghost")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSyncSubmodule_Success(t *testing.T) {
	// Create a child repo (acts as upstream) with an initial commit
	upstream := t.TempDir()
	initRepo(t, upstream)

	parent := t.TempDir()
	initRepo(t, parent)

	subName := "sub"
	if out, err := exec.Command("git", "-C", parent,
		"-c", "protocol.file.allow=always",
		"submodule", "add", upstream, subName).CombinedOutput(); err != nil {
		t.Fatalf("submodule add: %v\n%s", err, out)
	}
	exec.Command("git", "-C", parent, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", parent, "config", "user.name", "Test").Run()
	exec.Command("git", "-C", parent, "commit", "-am", "add submodule").Run()

	err := git.SyncSubmodule(context.Background(), parent, subName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListRemotes_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := git.ListRemotes(ctx, dir)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestAddRemote_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := git.AddRemote(ctx, dir, "origin", "https://example.com/repo.git")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
