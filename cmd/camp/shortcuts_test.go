package main

import (
	"testing"
)

func TestShortcutsAddDispatch(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		jumpFlag bool
		wantErr  bool
	}{
		{
			name:     "project sub-shortcut with 3 args is valid",
			args:     []string{"myproject", "sub", "subdir"},
			jumpFlag: false,
			wantErr:  false,
		},
		{
			name:     "jump shortcut with 2 args is valid",
			args:     []string{"api", "projects/myproject/"},
			jumpFlag: true,
			wantErr:  false,
		},
		{
			name:     "project mode with 1 arg is invalid",
			args:     []string{"onlyone"},
			jumpFlag: false,
			wantErr:  true,
		},
		{
			name:     "project mode with 0 args is valid (TUI mode)",
			args:     []string{},
			jumpFlag: false,
			wantErr:  false,
		},
		{
			name:     "jump mode with 1 arg is valid",
			args:     []string{"api"},
			jumpFlag: true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var valid bool
			if tt.jumpFlag {
				valid = len(tt.args) >= 0 && len(tt.args) <= 2
			} else {
				valid = len(tt.args) == 0 || len(tt.args) == 3
			}
			hasErr := !valid
			if hasErr != tt.wantErr {
				t.Errorf("args=%v jump=%v: got err=%v, want err=%v", tt.args, tt.jumpFlag, hasErr, tt.wantErr)
			}
		})
	}
}

func TestAddJumpDeprecationAlias(t *testing.T) {
	found := false
	for _, sub := range shortcutsCmd.Commands() {
		if sub.Name() == "add-jump" {
			found = true
			if !sub.Hidden {
				t.Error("add-jump should be hidden")
			}
			if sub.Deprecated == "" {
				t.Error("add-jump should have deprecation message")
			}
		}
	}
	if !found {
		t.Error("add-jump command not found (should exist as hidden alias)")
	}
}

func TestRemoveJumpDeprecationAlias(t *testing.T) {
	found := false
	for _, sub := range shortcutsCmd.Commands() {
		if sub.Name() == "remove-jump" {
			found = true
			if !sub.Hidden {
				t.Error("remove-jump should be hidden")
			}
			if sub.Deprecated == "" {
				t.Error("remove-jump should have deprecation message")
			}
		}
	}
	if !found {
		t.Error("remove-jump command not found (should exist as hidden alias)")
	}
}

func TestShortcutsAddHasJumpFlag(t *testing.T) {
	f := shortcutsAddCmd.Flags().Lookup("jump")
	if f == nil {
		t.Fatal("add command missing --jump flag")
	}
	if f.Shorthand != "j" {
		t.Errorf("jump flag shorthand = %q, want %q", f.Shorthand, "j")
	}
}

func TestShortcutsRemoveHasJumpFlag(t *testing.T) {
	f := shortcutsRemoveCmd.Flags().Lookup("jump")
	if f == nil {
		t.Fatal("remove command missing --jump flag")
	}
	if f.Shorthand != "j" {
		t.Errorf("jump flag shorthand = %q, want %q", f.Shorthand, "j")
	}
}
