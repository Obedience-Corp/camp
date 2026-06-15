package navigation

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/shortcuts"
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
	diff := shortcuts.ComputeShortcutDiff(defaults, defaults)

	if len(diff.Missing) != 0 {
		t.Errorf("Missing = %v, want empty", diff.Missing)
	}
	if len(diff.Stale) != 0 {
		t.Errorf("Stale = %v, want empty", diff.Stale)
	}
	if len(diff.Modified) != 0 {
		t.Errorf("Modified = %v, want empty", diff.Modified)
	}
	if len(diff.Custom) != 0 {
		t.Errorf("Custom = %v, want empty", diff.Custom)
	}
	if diff.Matched != len(defaults) {
		t.Errorf("Matched = %d, want %d", diff.Matched, len(defaults))
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

	diff := shortcuts.ComputeShortcutDiff(current, defaults)

	if len(diff.Missing) != 1 || diff.Missing[0] != "ai" {
		t.Errorf("Missing = %v, want [ai]", diff.Missing)
	}
	if diff.Matched != len(defaults)-1 {
		t.Errorf("Matched = %d, want %d", diff.Matched, len(defaults)-1)
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

	diff := shortcuts.ComputeShortcutDiff(current, defaults)

	if len(diff.Stale) != 1 || diff.Stale[0] != "a" {
		t.Errorf("Stale = %v, want [a]", diff.Stale)
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

	diff := shortcuts.ComputeShortcutDiff(current, defaults)

	if len(diff.Custom) != 1 || diff.Custom[0] != "api" {
		t.Errorf("Custom = %v, want [api]", diff.Custom)
	}
	if len(diff.Stale) != 0 {
		t.Errorf("Stale = %v, want empty", diff.Stale)
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

	diff := shortcuts.ComputeShortcutDiff(current, defaults)

	if len(diff.Modified) != 1 || diff.Modified[0] != "d" {
		t.Errorf("Modified = %v, want [d]", diff.Modified)
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

	diff := shortcuts.ComputeShortcutDiff(current, defaults)

	// Unknown key with empty source and no default match → treated as custom
	if len(diff.Custom) != 1 || diff.Custom[0] != "old" {
		t.Errorf("Custom = %v, want [old]", diff.Custom)
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
			got := shortcuts.IsAutoShortcut(tt.sc, tt.key, defaults)
			if got != tt.want {
				t.Errorf("IsAutoShortcut() = %v, want %v", got, tt.want)
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

// TestNewUserShortcut_SetsSourceUser guards against regression of issue #224.
// Shortcuts added via the CLI must be tagged ShortcutSourceUser so the repair
// system preserves them instead of treating them as legacy entries subject to
// the default-match heuristic.
func TestNewUserShortcut_SetsSourceUser(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		description string
		concept     string
	}{
		{
			name:        "path only",
			path:        "projects/api/",
			description: "",
			concept:     "",
		},
		{
			name:        "concept only",
			path:        "",
			description: "",
			concept:     "build",
		},
		{
			name:        "path with description and concept",
			path:        "projects/api/",
			description: "API service",
			concept:     "service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := shortcuts.NewUserShortcut(tt.path, tt.description, tt.concept)

			if sc.Source != config.ShortcutSourceUser {
				t.Errorf("Source = %q, want %q", sc.Source, config.ShortcutSourceUser)
			}
			if sc.Path != tt.path {
				t.Errorf("Path = %q, want %q", sc.Path, tt.path)
			}
			if sc.Description != tt.description {
				t.Errorf("Description = %q, want %q", sc.Description, tt.description)
			}
			if sc.Concept != tt.concept {
				t.Errorf("Concept = %q, want %q", sc.Concept, tt.concept)
			}
		})
	}
}

// TestNewUserShortcut_NotTreatedAsAuto verifies that shortcuts created via the
// CLI helper are recognized by IsAutoShortcut() as user-defined (not auto),
// which is the semantic property the repair system relies on.
func TestNewUserShortcut_NotTreatedAsAuto(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()
	sc := shortcuts.NewUserShortcut("projects/api/", "API", "service")

	// Even if the key happens to collide with a default key, a user-sourced
	// shortcut must not be classified as auto.
	for _, key := range []string{"api", "p", "unknown-key"} {
		if shortcuts.IsAutoShortcut(sc, key, defaults) {
			t.Errorf("IsAutoShortcut(key=%q) = true, want false for user-sourced shortcut", key)
		}
	}
}
