package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
)

func TestNewBranchConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		localExists  bool
		remoteExists bool
		wantNil      bool
		sentinel     error
	}{
		{name: "neither", wantNil: true},
		{name: "local leftover", localExists: true, sentinel: ErrBranchExists},
		{name: "origin shadow", remoteExists: true, sentinel: ErrRemoteBranchExists},
		{name: "local wins over origin", localExists: true, remoteExists: true, sentinel: ErrBranchExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newBranchConflict("camp", "judge-command-tools", tt.localExists, tt.remoteExists)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("newBranchConflict() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("newBranchConflict() = nil, want error")
			}
			if !errors.Is(err, tt.sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false", err, tt.sentinel)
			}
			if errors.Is(err, ErrBranchExists) && errors.Is(err, ErrRemoteBranchExists) {
				t.Fatal("sentinels must not match each other")
			}
		})
	}
}

// TestGuardCrossProjectSymlink_Refuses reproduces camp#245: when the
// worktree holder for project A is a symlink into project B's working tree,
// guardCrossProjectSymlink must refuse the worktree creation.
func TestGuardCrossProjectSymlink_Refuses(t *testing.T) {
	tmpDir := t.TempDir()

	// Two registered projects, each with a directory under projects/.
	projA := filepath.Join(tmpDir, "projects", "alpha")
	projB := filepath.Join(tmpDir, "projects", "beta")
	if err := os.MkdirAll(projA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projB, 0o755); err != nil {
		t.Fatal(err)
	}

	// The worktrees holder for alpha is a symlink into beta's tree.
	wtHolderParent := filepath.Join(tmpDir, "projects", "worktrees")
	if err := os.MkdirAll(wtHolderParent, 0o755); err != nil {
		t.Fatal(err)
	}
	// Symlink: projects/worktrees/alpha -> ../beta
	if err := os.Symlink(projB, filepath.Join(wtHolderParent, "alpha")); err != nil {
		t.Fatal(err)
	}

	cfg := &config.CampaignConfig{
		Projects: []config.ProjectConfig{
			{Name: "alpha", Path: "projects/alpha"},
			{Name: "beta", Path: "projects/beta"},
		},
	}
	resolver := paths.NewResolver(tmpDir, config.DefaultCampaignPaths())
	creator := NewCreator(resolver, cfg)

	// The logical worktree path crosses into beta via the symlink.
	logicalWtPath := creator.pathManager.WorktreePath("alpha", "feat-245")

	err := creator.guardCrossProjectSymlink("alpha", "feat-245", logicalWtPath)
	if err == nil {
		t.Fatal("guardCrossProjectSymlink() = nil, want error for cross-project symlink")
	}
	if !errors.Is(err, ErrCrossProjectSymlink) {
		t.Fatalf("errors.Is(err, ErrCrossProjectSymlink) = false, got %v", err)
	}
}

// TestGuardCrossProjectSymlink_AllowsNormal verifies the guard does not
// fire for a straightforward (non-symlinked) worktree holder.
func TestGuardCrossProjectSymlink_AllowsNormal(t *testing.T) {
	tmpDir := t.TempDir()

	projA := filepath.Join(tmpDir, "projects", "alpha")
	if err := os.MkdirAll(projA, 0o755); err != nil {
		t.Fatal(err)
	}
	// Normal worktrees dir — no symlink.
	wtDir := filepath.Join(tmpDir, "projects", "worktrees", "alpha")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.CampaignConfig{
		Projects: []config.ProjectConfig{
			{Name: "alpha", Path: "projects/alpha"},
		},
	}
	resolver := paths.NewResolver(tmpDir, config.DefaultCampaignPaths())
	creator := NewCreator(resolver, cfg)

	logicalWtPath := creator.pathManager.WorktreePath("alpha", "feat-245")
	err := creator.guardCrossProjectSymlink("alpha", "feat-245", logicalWtPath)
	if err != nil {
		t.Fatalf("guardCrossProjectSymlink() = %v, want nil for normal path", err)
	}
}
