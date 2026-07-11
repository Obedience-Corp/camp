package intent

import "testing"

// TestDeprecatedIntentSubcommands_HiddenAndDeprecated asserts convert, find,
// and rename are hidden from help with a deprecation notice while remaining
// invocable, per the 2026-07-09 CLI usage audit
// (decommission-dead-cli-surface-per-20260709-234257).
func TestDeprecatedIntentSubcommands_HiddenAndDeprecated(t *testing.T) {
	for _, name := range []string{"convert", "find", "rename"} {
		cmd := findIntentSubcommand(name)
		if cmd == nil {
			t.Fatalf("camp intent %s is not registered under camp intent", name)
		}
		if !cmd.Hidden {
			t.Errorf("camp intent %s: Hidden = false, want true", name)
		}
		if cmd.Deprecated == "" {
			t.Errorf("camp intent %s: Deprecated is empty, want a deprecation notice", name)
		}
	}
}

// TestActiveIntentSubcommands_StayVisible guards against accidentally hiding
// the intent verbs that carry the product (add/list/move/promote and friends).
func TestActiveIntentSubcommands_StayVisible(t *testing.T) {
	for _, name := range []string{"add", "list", "move", "promote", "show", "edit", "archive"} {
		cmd := findIntentSubcommand(name)
		if cmd == nil {
			t.Fatalf("camp intent %s is not registered under camp intent", name)
		}
		if cmd.Hidden {
			t.Errorf("camp intent %s: Hidden = true, want false", name)
		}
	}
}
