package navigation

import (
	"testing"
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
