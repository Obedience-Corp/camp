package index

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
)

func TestResolveRunShortcut_ConsumesProjectSubShortcut(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	_, cliDir := writeRunShortcutIndex(t, root)
	cfg := runShortcutConfig(map[string]config.ShortcutConfig{
		"p": {Path: "projects/"},
	})

	resolution, err := ResolveRunShortcut(ctx, root, cfg, "p", []string{"camp", "cli", "go", "test"})
	if err != nil {
		t.Fatalf("ResolveRunShortcut() error = %v", err)
	}

	if resolution.WorkDir != cliDir {
		t.Fatalf("WorkDir = %q, want %q", resolution.WorkDir, cliDir)
	}
	if !slices.Equal(resolution.CommandArgs, []string{"go", "test"}) {
		t.Fatalf("CommandArgs = %#v, want %#v", resolution.CommandArgs, []string{"go", "test"})
	}
	if !resolution.BypassProjectDispatch {
		t.Fatal("BypassProjectDispatch = false, want true")
	}
}

func TestResolveRunShortcut_LeavesUnknownSubShortcutAsCommandArg(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir, _ := writeRunShortcutIndex(t, root)
	cfg := runShortcutConfig(map[string]config.ShortcutConfig{
		"p": {Path: "projects/"},
	})

	resolution, err := ResolveRunShortcut(ctx, root, cfg, "p", []string{"camp", "test", "./..."})
	if err != nil {
		t.Fatalf("ResolveRunShortcut() error = %v", err)
	}

	if resolution.WorkDir != projectDir {
		t.Fatalf("WorkDir = %q, want %q", resolution.WorkDir, projectDir)
	}
	if !slices.Equal(resolution.CommandArgs, []string{"test", "./..."}) {
		t.Fatalf("CommandArgs = %#v, want %#v", resolution.CommandArgs, []string{"test", "./..."})
	}
	if !resolution.BypassProjectDispatch {
		t.Fatal("BypassProjectDispatch = false, want true")
	}
}

func TestResolveRunShortcut_CustomPathUsesNormalDispatch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	customDir := filepath.Join(root, "custom", "scripts")
	mkdirAll(t, filepath.Join(root, ".campaign"))
	mkdirAll(t, customDir)
	cfg := runShortcutConfig(map[string]config.ShortcutConfig{
		"custom": {Path: "custom/scripts/"},
	})

	resolution, err := ResolveRunShortcut(ctx, root, cfg, "custom", []string{"camp", "test"})
	if err != nil {
		t.Fatalf("ResolveRunShortcut() error = %v", err)
	}

	if resolution.WorkDir != customDir {
		t.Fatalf("WorkDir = %q, want %q", resolution.WorkDir, customDir)
	}
	if !slices.Equal(resolution.CommandArgs, []string{"camp", "test"}) {
		t.Fatalf("CommandArgs = %#v, want %#v", resolution.CommandArgs, []string{"camp", "test"})
	}
	if resolution.BypassProjectDispatch {
		t.Fatal("BypassProjectDispatch = true, want false")
	}
}

func writeRunShortcutIndex(t *testing.T, root string) (string, string) {
	t.Helper()

	projectDir := filepath.Join(root, "projects", "camp")
	cliDir := filepath.Join(projectDir, "cmd", "camp")
	mkdirAll(t, filepath.Join(root, ".campaign"))
	mkdirAll(t, cliDir)

	idx := NewIndex(root)
	idx.AddTarget(Target{
		Name:     "camp",
		Path:     projectDir,
		Category: nav.CategoryProjects,
		Shortcuts: map[string]string{
			"cli": "cmd/camp",
		},
	})
	if err := Save(idx, root); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	return projectDir, cliDir
}

func runShortcutConfig(shortcuts map[string]config.ShortcutConfig) *config.CampaignConfig {
	return &config.CampaignConfig{
		Jumps: &config.JumpsConfig{
			Shortcuts: shortcuts,
		},
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
}
