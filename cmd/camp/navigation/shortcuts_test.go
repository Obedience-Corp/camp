package navigation

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestShortcutsAddDispatch(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "0 args is valid (TUI mode)",
			args:    []string{},
			wantErr: false,
		},
		{
			name:    "1 arg is invalid",
			args:    []string{"onlyone"},
			wantErr: true,
		},
		{
			name:    "2 args is valid (campaign shortcut)",
			args:    []string{"api", "projects/api/"},
			wantErr: false,
		},
		{
			name:    "3 args is valid (project sub-shortcut)",
			args:    []string{"myproject", "sub", "subdir"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := len(tt.args) == 0 || len(tt.args) == 2 || len(tt.args) == 3
			hasErr := !valid
			if hasErr != tt.wantErr {
				t.Errorf("args=%v: got err=%v, want err=%v", tt.args, hasErr, tt.wantErr)
			}
		})
	}
}

func TestShortcutsAddHasMetadataFlags(t *testing.T) {
	for _, flag := range []struct {
		name      string
		shorthand string
	}{
		{"description", "d"},
		{"concept", "c"},
	} {
		f := shortcutsAddCmd.Flags().Lookup(flag.name)
		if f == nil {
			t.Fatalf("add command missing --%s flag", flag.name)
		}
		if f.Shorthand != flag.shorthand {
			t.Errorf("--%s shorthand = %q, want %q", flag.name, f.Shorthand, flag.shorthand)
		}
	}
}

func TestShortcutsAddNoJumpFlag(t *testing.T) {
	f := shortcutsAddCmd.Flags().Lookup("jump")
	if f != nil {
		t.Error("add command should not have --jump flag")
	}
}

func TestShortcutsRemoveNoJumpFlag(t *testing.T) {
	f := shortcutsRemoveCmd.Flags().Lookup("jump")
	if f != nil {
		t.Error("remove command should not have --jump flag")
	}
}

func TestDeprecatedCommandsRemoved(t *testing.T) {
	for _, sub := range ShortcutsCmd.Commands() {
		if sub.Name() == "add-jump" {
			t.Error("add-jump command should not exist")
		}
		if sub.Name() == "remove-jump" {
			t.Error("remove-jump command should not exist")
		}
	}
}

func TestComputeShortcutDiff_AllMatch(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()
	diff := computeShortcutDiff(defaults, defaults)

	if len(diff.missing) != 0 {
		t.Errorf("missing = %v, want empty", diff.missing)
	}
	if len(diff.stale) != 0 {
		t.Errorf("stale = %v, want empty", diff.stale)
	}
	if len(diff.modified) != 0 {
		t.Errorf("modified = %v, want empty", diff.modified)
	}
	if len(diff.custom) != 0 {
		t.Errorf("custom = %v, want empty", diff.custom)
	}
	if diff.matched != len(defaults) {
		t.Errorf("matched = %d, want %d", diff.matched, len(defaults))
	}
}

func TestComputeShortcutDiff_MissingDefaults(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()

	// Current config is missing the "ai" shortcut
	current := make(map[string]config.ShortcutConfig)
	for k, v := range defaults {
		if k != "ai" {
			current[k] = v
		}
	}

	diff := computeShortcutDiff(current, defaults)

	if len(diff.missing) != 1 || diff.missing[0] != "ai" {
		t.Errorf("missing = %v, want [ai]", diff.missing)
	}
	if diff.matched != len(defaults)-1 {
		t.Errorf("matched = %d, want %d", diff.matched, len(defaults)-1)
	}
}

func TestComputeShortcutDiff_StaleAutoShortcut(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()

	// Current config has old "a" shortcut (auto-generated, not in defaults)
	current := make(map[string]config.ShortcutConfig)
	for k, v := range defaults {
		current[k] = v
	}
	current["a"] = config.ShortcutConfig{
		Path:   "ai_docs/",
		Source: config.ShortcutSourceAuto,
	}

	diff := computeShortcutDiff(current, defaults)

	if len(diff.stale) != 1 || diff.stale[0] != "a" {
		t.Errorf("stale = %v, want [a]", diff.stale)
	}
}

func TestComputeShortcutDiff_CustomShortcutPreserved(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()

	current := make(map[string]config.ShortcutConfig)
	for k, v := range defaults {
		current[k] = v
	}
	current["api"] = config.ShortcutConfig{
		Path:   "projects/api/",
		Source: config.ShortcutSourceUser,
	}

	diff := computeShortcutDiff(current, defaults)

	if len(diff.custom) != 1 || diff.custom[0] != "api" {
		t.Errorf("custom = %v, want [api]", diff.custom)
	}
	if len(diff.stale) != 0 {
		t.Errorf("stale = %v, want empty", diff.stale)
	}
}

func TestComputeShortcutDiff_ModifiedAutoShortcut(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()

	current := make(map[string]config.ShortcutConfig)
	for k, v := range defaults {
		current[k] = v
	}
	// Modify an auto shortcut's path
	modified := current["d"]
	modified.Path = "documentation/"
	modified.Source = config.ShortcutSourceAuto
	current["d"] = modified

	diff := computeShortcutDiff(current, defaults)

	if len(diff.modified) != 1 || diff.modified[0] != "d" {
		t.Errorf("modified = %v, want [d]", diff.modified)
	}
}

func TestComputeShortcutDiff_LegacyEmptySource(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()

	// Legacy shortcut with empty source that matches a default — should be treated as auto
	current := make(map[string]config.ShortcutConfig)
	for k, v := range defaults {
		current[k] = v
	}
	// Add a legacy entry not in defaults — empty source, unknown key
	current["old"] = config.ShortcutConfig{
		Path:   "old_stuff/",
		Source: "", // legacy, no source
	}

	diff := computeShortcutDiff(current, defaults)

	// Unknown key with empty source and no default match → treated as custom
	if len(diff.custom) != 1 || diff.custom[0] != "old" {
		t.Errorf("custom = %v, want [old]", diff.custom)
	}
}

func TestIsAutoShortcut(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()

	tests := []struct {
		name string
		sc   config.ShortcutConfig
		key  string
		want bool
	}{
		{
			name: "explicit auto source",
			sc:   config.ShortcutConfig{Source: config.ShortcutSourceAuto, Path: "anything/"},
			key:  "x",
			want: true,
		},
		{
			name: "explicit user source",
			sc:   config.ShortcutConfig{Source: config.ShortcutSourceUser, Path: "anything/"},
			key:  "x",
			want: false,
		},
		{
			name: "legacy matching default",
			sc:   config.ShortcutConfig{Source: "", Path: "docs/"},
			key:  "d",
			want: true,
		},
		{
			name: "legacy not matching any default",
			sc:   config.ShortcutConfig{Source: "", Path: "custom/"},
			key:  "z",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAutoShortcut(tt.sc, tt.key, defaults)
			if got != tt.want {
				t.Errorf("isAutoShortcut() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiffAndResetCommandsRegistered(t *testing.T) {
	found := map[string]bool{"diff": false, "reset": false}
	for _, sub := range ShortcutsCmd.Commands() {
		if _, ok := found[sub.Name()]; ok {
			found[sub.Name()] = true
		}
	}
	for name, ok := range found {
		if !ok {
			t.Errorf("subcommand %q not registered on ShortcutsCmd", name)
		}
	}
}

func TestResetHasFlags(t *testing.T) {
	for _, flag := range []string{"all", "dry-run"} {
		f := shortcutsResetCmd.Flags().Lookup(flag)
		if f == nil {
			t.Errorf("reset command missing --%s flag", flag)
		}
	}
}
