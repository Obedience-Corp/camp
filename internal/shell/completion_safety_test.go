package shell

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

// TestCompletionOutputNoEscapeSequences verifies that all shell completion
// invocations include NO_COLOR=1 to prevent lipgloss/termenv from sending
// OSC escape sequences to /dev/tty during shell completion. Without this,
// terminal query responses leak into zsh's input buffer, corrupting the
// completion state machine and breaking the user's shell environment
// (commands like git stop working).
//
// This test exists because this bug has been reintroduced multiple times.
// If this test fails, the fix is to add "NO_COLOR=1" before every
// "command camp complete" invocation in the shell init scripts.
func TestCompletionOutputNoEscapeSequences(t *testing.T) {
	shells := []struct {
		name     string
		generate func() string
	}{
		{"zsh", generateZsh},
		{"bash", generateBash},
		{"fish", generateFish},
	}

	for _, shell := range shells {
		t.Run(shell.name, func(t *testing.T) {
			output := shell.generate()

			// Every "camp complete" invocation must be prefixed with
			// NO_COLOR=1 and "command" (to bypass shell function wrappers)
			lines := strings.Split(output, "\n")
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)

				// Skip comments
				if strings.HasPrefix(trimmed, "#") {
					continue
				}

				// Find lines that invoke "camp complete"
				if !strings.Contains(trimmed, "camp complete") {
					continue
				}

				// Must have NO_COLOR=1 prefix
				if !strings.Contains(trimmed, "NO_COLOR=1") {
					t.Errorf("line %d: 'camp complete' invocation missing NO_COLOR=1 prefix (prevents terminal OSC query leaks):\n  %s", i+1, trimmed)
				}

				// Must use "command camp" (not bare "camp") to bypass shell function
				if !strings.Contains(trimmed, "command camp complete") {
					t.Errorf("line %d: 'camp complete' invocation missing 'command' prefix (bypasses shell function wrapper):\n  %s", i+1, trimmed)
				}
			}
		})
	}
}

// TestZshCompletionNoProcessSubstitution verifies that the zsh completion
// function does NOT use process substitution `< <(...)`. Process substitution
// inside zsh completion functions creates temporary named pipes that compete
// with zsh's internal fd management, corrupting the completion state machine
// and breaking the user's shell (ls, git, mv all stop working).
// Use command substitution `$(...)` instead.
func TestZshCompletionNoProcessSubstitution(t *testing.T) {
	output := generateZsh()
	if strings.Contains(output, "< <(") {
		t.Error("zsh completion uses process substitution '< <(...)' which corrupts zsh's completion state machine; use command substitution '$()' instead")
	}
}

// TestShortcutConsistencyAcrossShells verifies that all shell init scripts
// contain the same set of navigation shortcuts, sourced from the same
// config.DefaultNavigationShortcuts() data. Hardcoded shortcut lists
// get out of sync (e.g., "pw" vs "wt" for worktrees) and cause confusing
// behavior where tab completion shows different options than what actually works.
func TestShortcutConsistencyAcrossShells(t *testing.T) {
	defaults := config.DefaultNavigationShortcuts()

	// Collect all navigation shortcut keys
	var navKeys []string
	for key, sc := range defaults {
		if sc.IsNavigation() {
			navKeys = append(navKeys, key)
		}
	}

	if len(navKeys) == 0 {
		t.Fatal("no navigation shortcuts found in defaults")
	}

	shells := []struct {
		name     string
		generate func() string
	}{
		{"zsh", generateZsh},
		{"bash", generateBash},
		{"fish", generateFish},
	}

	for _, shell := range shells {
		t.Run(shell.name, func(t *testing.T) {
			output := shell.generate()
			for _, key := range navKeys {
				if !strings.Contains(output, key) {
					t.Errorf("%s init missing navigation shortcut: %q", shell.name, key)
				}
			}
		})
	}
}

// TestNavigationShortcutsMatchDefaults verifies that the navigationShortcuts()
// helper returns exactly the navigation shortcuts from DefaultNavigationShortcuts(),
// with no extras and no omissions.
func TestNavigationShortcutsMatchDefaults(t *testing.T) {
	entries := navigationShortcuts()
	defaults := config.DefaultNavigationShortcuts()

	// Count expected nav shortcuts
	expectedCount := 0
	for _, sc := range defaults {
		if sc.IsNavigation() {
			expectedCount++
		}
	}

	if len(entries) != expectedCount {
		t.Errorf("navigationShortcuts() returned %d entries, expected %d", len(entries), expectedCount)
	}

	// Verify each entry matches a default
	entryMap := make(map[string]string)
	for _, e := range entries {
		entryMap[e.Key] = e.Description
	}

	for key, sc := range defaults {
		if !sc.IsNavigation() {
			continue
		}
		if _, ok := entryMap[key]; !ok {
			t.Errorf("navigationShortcuts() missing key: %s", key)
		}
	}

	// Verify no extras
	for _, e := range entries {
		sc, ok := defaults[e.Key]
		if !ok {
			t.Errorf("navigationShortcuts() has key %q not in defaults", e.Key)
			continue
		}
		if !sc.IsNavigation() {
			t.Errorf("navigationShortcuts() includes non-navigation key: %s", e.Key)
		}
	}
}

// TestNavigationShortcutsAreSorted verifies that shortcuts are returned
// in alphabetical order for consistent shell completion ordering.
func TestNavigationShortcutsAreSorted(t *testing.T) {
	entries := navigationShortcuts()
	for i := 1; i < len(entries); i++ {
		if entries[i].Key < entries[i-1].Key {
			t.Errorf("shortcuts not sorted: %q comes after %q", entries[i].Key, entries[i-1].Key)
		}
	}
}
