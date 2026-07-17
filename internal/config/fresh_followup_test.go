package config

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestFollowUpConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		entry   FollowUpConfig
		wantErr bool
	}{
		{
			name:    "empty run is rejected",
			entry:   FollowUpConfig{Name: "install", Run: ""},
			wantErr: true,
		},
		{
			name:    "whitespace-only run is rejected",
			entry:   FollowUpConfig{Name: "install", Run: "   "},
			wantErr: true,
		},
		{
			name:    "empty name is rejected",
			entry:   FollowUpConfig{Name: "", Run: "npm install"},
			wantErr: true,
		},
		{
			name:    "well-formed entry is accepted",
			entry:   FollowUpConfig{Name: "install", Run: "npm install"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.entry.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestReorderFreshFollowUps(t *testing.T) {
	entries := []FollowUpConfig{
		{Name: "install", Run: "npm install"},
		{Name: "generate", Run: "npm run generate"},
		{Name: "build", Run: "npm run build"},
	}

	tests := []struct {
		name  string
		step  string
		delta int
		want  []string
	}{
		{name: "up", step: "build", delta: -1, want: []string{"install", "build", "generate"}},
		{name: "down", step: "install", delta: 1, want: []string{"generate", "install", "build"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReorderFreshFollowUps(entries, tt.step, tt.delta)
			if err != nil {
				t.Fatalf("ReorderFreshFollowUps() error = %v", err)
			}
			var names []string
			for _, entry := range got {
				names = append(names, entry.Name)
			}
			if !reflect.DeepEqual(names, tt.want) {
				t.Fatalf("reordered names = %v, want %v", names, tt.want)
			}
		})
	}

	if !reflect.DeepEqual(entries, []FollowUpConfig{
		{Name: "install", Run: "npm install"},
		{Name: "generate", Run: "npm run generate"},
		{Name: "build", Run: "npm run build"},
	}) {
		t.Fatal("ReorderFreshFollowUps mutated its input")
	}
}

func TestReorderFreshFollowUpsRejectsInvalidMoves(t *testing.T) {
	entries := []FollowUpConfig{{Name: "install", Run: "npm install"}}
	for _, tt := range []struct {
		name  string
		step  string
		delta int
	}{
		{name: "already first", step: "install", delta: -1},
		{name: "already last", step: "install", delta: 1},
		{name: "unknown step", step: "missing", delta: -1},
		{name: "invalid delta", step: "install", delta: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ReorderFreshFollowUps(entries, tt.step, tt.delta); err == nil {
				t.Fatal("ReorderFreshFollowUps() error = nil, want error")
			}
		})
	}
}

func TestResolveFreshFollowUps_Precedence(t *testing.T) {
	global := []FollowUpConfig{{Name: "install", Run: "npm install"}}
	override := []FollowUpConfig{{Name: "build", Run: "make build"}}

	cfg := &FreshConfig{
		FollowUp: global,
		Projects: map[string]FreshProjectConfig{
			"camp":  {FollowUp: override},
			"empty": {FollowUp: []FollowUpConfig{}},
		},
	}

	t.Run("project override replaces global entirely", func(t *testing.T) {
		got := cfg.ResolveFreshFollowUps("camp")
		if len(got) != 1 || got[0].Name != "build" {
			t.Fatalf("ResolveFreshFollowUps(camp) = %+v, want [build]", got)
		}
	})

	t.Run("explicit empty override suppresses global follow-ups", func(t *testing.T) {
		got := cfg.ResolveFreshFollowUps("empty")
		if len(got) != 0 {
			t.Fatalf("ResolveFreshFollowUps(empty) = %+v, want empty", got)
		}
	})

	t.Run("project without an override falls back to global", func(t *testing.T) {
		got := cfg.ResolveFreshFollowUps("fest")
		if len(got) != 1 || got[0].Name != "install" {
			t.Fatalf("ResolveFreshFollowUps(fest) = %+v, want [install]", got)
		}
	})
}

func TestAddFreshFollowUp_RejectsInvalidEntry(t *testing.T) {
	root := t.TempDir()

	err := AddFreshFollowUp(context.Background(), root, "", FollowUpConfig{Name: "install", Run: ""})
	if err == nil {
		t.Fatal("AddFreshFollowUp() with empty run = nil, want error")
	}

	if _, statErr := os.Stat(FreshConfigPath(root)); !os.IsNotExist(statErr) {
		t.Fatal("AddFreshFollowUp() should not create fresh.yaml when validation fails")
	}
}

func TestAddFreshFollowUp_CreatesFileWhenMissing(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	err := AddFreshFollowUp(ctx, root, "", FollowUpConfig{Name: "install", Run: "npm install"})
	if err != nil {
		t.Fatalf("AddFreshFollowUp() error = %v", err)
	}

	cfg, err := LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	if len(cfg.FollowUp) != 1 || cfg.FollowUp[0].Name != "install" || cfg.FollowUp[0].Run != "npm install" {
		t.Fatalf("cfg.FollowUp = %+v, want single install step", cfg.FollowUp)
	}
}

func TestAddFreshFollowUp_ProjectScope(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	err := AddFreshFollowUp(ctx, root, "camp", FollowUpConfig{
		Name: "build",
		Run:  "go build ./...",
		Dir:  "cmd/camp",
	})
	if err != nil {
		t.Fatalf("AddFreshFollowUp() error = %v", err)
	}

	cfg, err := LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	if len(cfg.FollowUp) != 0 {
		t.Fatalf("global FollowUp = %+v, want empty (step was project-scoped)", cfg.FollowUp)
	}
	pc, ok := cfg.Projects["camp"]
	if !ok || len(pc.FollowUp) != 1 || pc.FollowUp[0].Name != "build" || pc.FollowUp[0].Dir != "cmd/camp" {
		t.Fatalf("cfg.Projects[camp].FollowUp = %+v, want single build step with dir", pc.FollowUp)
	}
}

func TestAddFreshFollowUp_PreservesUnknownKeysAndExistingSettings(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	settingsDir := SettingsDirPath(root)
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}
	initial := "branch: develop\n" +
		"push_upstream: false\n" +
		"custom_future_key: keep-me\n"
	if err := os.WriteFile(FreshConfigPath(root), []byte(initial), 0o644); err != nil {
		t.Fatalf("failed to seed fresh config: %v", err)
	}

	if err := AddFreshFollowUp(ctx, root, "", FollowUpConfig{Name: "install", Run: "npm install"}); err != nil {
		t.Fatalf("AddFreshFollowUp() error = %v", err)
	}

	raw, err := os.ReadFile(FreshConfigPath(root))
	if err != nil {
		t.Fatalf("failed to read fresh config: %v", err)
	}
	rawStr := string(raw)
	if !strings.Contains(rawStr, "custom_future_key: keep-me") {
		t.Fatalf("fresh.yaml lost an unrecognized top-level key:\n%s", rawStr)
	}

	cfg, err := LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	if cfg.Branch != "develop" {
		t.Fatalf("cfg.Branch = %q, want %q (existing setting must survive the write)", cfg.Branch, "develop")
	}
	if cfg.PushUpstream == nil || *cfg.PushUpstream {
		t.Fatalf("cfg.PushUpstream = %v, want false (existing setting must survive the write)", cfg.PushUpstream)
	}
	if len(cfg.FollowUp) != 1 || cfg.FollowUp[0].Name != "install" {
		t.Fatalf("cfg.FollowUp = %+v, want single install step", cfg.FollowUp)
	}
}

func TestAddFreshFollowUp_PreservesCommentOnlyHeaderOnFirstWrite(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	settingsDir := SettingsDirPath(root)
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}
	header := "# fresh.yaml -- scaffolded defaults, fully commented out.\n# branch: develop\n"
	if err := os.WriteFile(FreshConfigPath(root), []byte(header), 0o644); err != nil {
		t.Fatalf("failed to seed fresh config: %v", err)
	}

	if err := AddFreshFollowUp(ctx, root, "", FollowUpConfig{Name: "install", Run: "npm install"}); err != nil {
		t.Fatalf("AddFreshFollowUp() error = %v", err)
	}

	raw, err := os.ReadFile(FreshConfigPath(root))
	if err != nil {
		t.Fatalf("failed to read fresh config: %v", err)
	}
	rawStr := string(raw)
	if !strings.Contains(rawStr, "# fresh.yaml -- scaffolded defaults, fully commented out.") {
		t.Fatalf("fresh.yaml lost its comment-only header on first write:\n%s", rawStr)
	}

	cfg, err := LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	if len(cfg.FollowUp) != 1 || cfg.FollowUp[0].Name != "install" {
		t.Fatalf("cfg.FollowUp = %+v, want single install step", cfg.FollowUp)
	}
}

func TestAddFreshFollowUp_RejectsDuplicateName(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	if err := AddFreshFollowUp(ctx, root, "", FollowUpConfig{Name: "install", Run: "npm install"}); err != nil {
		t.Fatalf("first AddFreshFollowUp() error = %v", err)
	}

	err := AddFreshFollowUp(ctx, root, "", FollowUpConfig{Name: "install", Run: "npm ci"})
	if err == nil {
		t.Fatal("second AddFreshFollowUp() with duplicate name = nil, want error")
	}

	cfg, err := LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	if len(cfg.FollowUp) != 1 || cfg.FollowUp[0].Run != "npm install" {
		t.Fatalf("cfg.FollowUp = %+v, want the original entry left untouched", cfg.FollowUp)
	}
}

func TestRemoveFreshFollowUp_RemovesEntryAndPrunesEmptyContainers(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	if err := AddFreshFollowUp(ctx, root, "camp", FollowUpConfig{Name: "build", Run: "go build ./..."}); err != nil {
		t.Fatalf("AddFreshFollowUp() error = %v", err)
	}

	if err := RemoveFreshFollowUp(ctx, root, "camp", "build"); err != nil {
		t.Fatalf("RemoveFreshFollowUp() error = %v", err)
	}

	raw, err := os.ReadFile(FreshConfigPath(root))
	if err != nil {
		t.Fatalf("failed to read fresh config: %v", err)
	}
	rawStr := string(raw)
	if strings.Contains(rawStr, "follow_up") || strings.Contains(rawStr, "projects") {
		t.Fatalf("expected empty follow_up/projects containers to be pruned, got:\n%s", rawStr)
	}

	cfg, err := LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	if len(cfg.Projects) != 0 {
		t.Fatalf("cfg.Projects = %+v, want empty after pruning", cfg.Projects)
	}
}

func TestRemoveFreshFollowUp_UnknownNameListsValidNames(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	if err := AddFreshFollowUp(ctx, root, "", FollowUpConfig{Name: "install", Run: "npm install"}); err != nil {
		t.Fatalf("AddFreshFollowUp() error = %v", err)
	}

	err := RemoveFreshFollowUp(ctx, root, "", "does-not-exist")
	if err == nil {
		t.Fatal("RemoveFreshFollowUp() with unknown name = nil, want error")
	}

	var notFound *FollowUpNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("RemoveFreshFollowUp() error = %v (%T), want *FollowUpNotFoundError", err, err)
	}
	if len(notFound.ValidNames) != 1 || notFound.ValidNames[0] != "install" {
		t.Fatalf("notFound.ValidNames = %v, want [install]", notFound.ValidNames)
	}
}

func TestRemoveFreshFollowUp_MissingFileReportsNoneConfigured(t *testing.T) {
	root := t.TempDir()

	err := RemoveFreshFollowUp(context.Background(), root, "", "install")
	if err == nil {
		t.Fatal("RemoveFreshFollowUp() on a campaign with no fresh.yaml = nil, want error")
	}

	var notFound *FollowUpNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("RemoveFreshFollowUp() error = %v (%T), want *FollowUpNotFoundError", err, err)
	}
	if len(notFound.ValidNames) != 0 {
		t.Fatalf("notFound.ValidNames = %v, want empty", notFound.ValidNames)
	}
}

func TestAddFreshFollowUp_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := AddFreshFollowUp(ctx, t.TempDir(), "", FollowUpConfig{Name: "install", Run: "npm install"})
	if err == nil {
		t.Fatal("AddFreshFollowUp() expected context error")
	}
}

func TestRemoveFreshFollowUp_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RemoveFreshFollowUp(ctx, t.TempDir(), "", "install")
	if err == nil {
		t.Fatal("RemoveFreshFollowUp() expected context error")
	}
}

func TestSetFreshFollowUps_PreservesInheritedStepsForProjectEdits(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	global := []FollowUpConfig{{Name: "install", Run: "npm install"}}
	if err := SetFreshFollowUps(ctx, root, "", global); err != nil {
		t.Fatalf("SetFreshFollowUps(global) error = %v", err)
	}

	projectSteps := append(append([]FollowUpConfig(nil), global...), FollowUpConfig{Name: "build", Run: "npm run build"})
	if err := SetFreshFollowUps(ctx, root, "web-app", projectSteps); err != nil {
		t.Fatalf("SetFreshFollowUps(project) error = %v", err)
	}

	cfg, err := LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	got := cfg.ResolveFreshFollowUps("web-app")
	if len(got) != 2 || got[0].Name != "install" || got[1].Name != "build" {
		t.Fatalf("project follow-ups = %+v, want inherited install followed by build", got)
	}
}

func TestSetFreshFollowUps_ProjectEmptyListRemainsExplicit(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	if err := SetFreshFollowUps(ctx, root, "web-app", nil); err != nil {
		t.Fatalf("SetFreshFollowUps(empty project) error = %v", err)
	}

	cfg, err := LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	steps, ok := cfg.Projects["web-app"]
	if !ok || steps.FollowUp == nil {
		t.Fatalf("project empty override = %+v, want explicit empty follow_up list", cfg.Projects)
	}
}
