package main

import (
	"context"
	"os"
	"strings"
	"testing"

	cachepkg "github.com/Obedience-Corp/camp/cmd/camp/cache"
	dungeonpkg "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	intentpkg "github.com/Obedience-Corp/camp/cmd/camp/intent"
	leveragepkg "github.com/Obedience-Corp/camp/cmd/camp/leverage"
	navigationpkg "github.com/Obedience-Corp/camp/cmd/camp/navigation"
	projectpkg "github.com/Obedience-Corp/camp/cmd/camp/project"
	projectworktreepkg "github.com/Obedience-Corp/camp/cmd/camp/project/worktree"
	refspkg "github.com/Obedience-Corp/camp/cmd/camp/refs"
	registrypkg "github.com/Obedience-Corp/camp/cmd/camp/registry"
	skillspkg "github.com/Obedience-Corp/camp/cmd/camp/skills"
	worktreespkg "github.com/Obedience-Corp/camp/cmd/camp/worktrees"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestRootRegistersPackageOwnedCommandPointers(t *testing.T) {
	want := []*cobra.Command{
		skillspkg.Cmd,
		cachepkg.Cmd,
		navigationpkg.Cmd,
		registrypkg.Cmd,
		projectpkg.Cmd,
		dungeonpkg.Cmd,
		intentpkg.Cmd,
		leveragepkg.Cmd,
		worktreespkg.Cmd,
		refspkg.Cmd,
	}

	for _, cmd := range want {
		if !hasCommandPointer(rootCmd, cmd) {
			t.Fatalf("root command does not register package-owned %q pointer", cmd.Name())
		}
	}
}

func TestProjectRegistersPackageOwnedWorktreePointer(t *testing.T) {
	if !hasCommandPointer(projectpkg.Cmd, projectworktreepkg.Cmd) {
		t.Fatal("project command does not register package-owned worktree pointer")
	}
}

func TestPackageOwnedRootMetadataPreserved(t *testing.T) {
	if navigationpkg.Cmd.GroupID != "navigation" {
		t.Fatalf("navigation GroupID = %q, want navigation", navigationpkg.Cmd.GroupID)
	}
	if !hasAlias(navigationpkg.Cmd, "g") {
		t.Fatal("navigation command missing g alias")
	}

	if registrypkg.Cmd.GroupID != "registry" {
		t.Fatalf("registry GroupID = %q, want registry", registrypkg.Cmd.GroupID)
	}
	if !hasAlias(registrypkg.Cmd, "reg") {
		t.Fatal("registry command missing reg alias")
	}

	if worktreespkg.Cmd.GroupID != "project" {
		t.Fatalf("worktrees GroupID = %q, want project", worktreespkg.Cmd.GroupID)
	}
	if !hasAlias(worktreespkg.Cmd, "wt") {
		t.Fatal("worktrees command missing wt alias")
	}
	if worktreespkg.Cmd.Deprecated == "" {
		t.Fatal("worktrees command lost deprecation text")
	}

	if !hasAlias(projectworktreepkg.Cmd, "wt") {
		t.Fatal("project worktree command missing wt alias")
	}
}

func TestNavigationRootUsesMainHandler(t *testing.T) {
	resetCommandFlags(t, navigationpkg.Cmd)

	tempDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	navigationpkg.Cmd.SetContext(context.Background())
	err = navigationpkg.Cmd.RunE(navigationpkg.Cmd, nil)
	if err == nil {
		t.Fatal("expected navigation root to execute main handler and fail outside a campaign")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "campaign") {
		t.Fatalf("navigation root returned unexpected error: %v", err)
	}
}

func TestRefsSyncRootUsesMainHandler(t *testing.T) {
	resetCommandFlags(t, refspkg.Cmd)

	tempDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	refspkg.Cmd.SetContext(context.Background())
	err = refspkg.Cmd.RunE(refspkg.Cmd, nil)
	if err == nil {
		t.Fatal("expected refs-sync root to execute main handler and fail outside a campaign")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "campaign") {
		t.Fatalf("refs-sync root returned unexpected error: %v", err)
	}
}

func hasCommandPointer(parent *cobra.Command, want *cobra.Command) bool {
	for _, child := range parent.Commands() {
		if child == want {
			return true
		}
	}
	return false
}

func hasAlias(cmd *cobra.Command, want string) bool {
	for _, alias := range cmd.Aliases {
		if alias == want {
			return true
		}
	}
	return false
}

func resetCommandFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()

	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		flag.Changed = false
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %s: %v", flag.Name, err)
		}
	})
}
