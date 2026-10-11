package explore

import "testing"

func TestChooseProtocolInsideMultiplexer(t *testing.T) {
	outer := map[string]map[string]string{
		"kitty":   {"KITTY_WINDOW_ID": "1"},
		"ghostty": {"TERM": "xterm-ghostty"},
		"wezterm": {"WEZTERM_PANE": "0"},
		"iterm":   {"LC_TERMINAL": "iTerm2"},
	}
	inner := map[string]map[string]string{
		"tmux":        {"TMUX": "/tmp/tmux-501/default,1,0", "TERM": "xterm-256color"},
		"tmux term":   {"TERM": "tmux-256color"},
		"screen":      {"STY": "1234.pts-0.host", "TERM": "xterm-256color"},
		"screen term": {"TERM": "screen-256color"},
		"zellij":      {"ZELLIJ": "0", "TERM": "xterm-256color"},
	}
	for outerName, outerEnv := range outer {
		direct := func(key string) string { return outerEnv[key] }
		want, err := ChooseProtocol(ProtocolAuto, direct, false)
		if err != nil || want == ProtocolOff {
			t.Fatalf("%s outside a multiplexer = %q, %v", outerName, want, err)
		}
		for innerName, innerEnv := range inner {
			getenv := func(key string) string {
				if value, ok := innerEnv[key]; ok {
					return value
				}
				return outerEnv[key]
			}
			got, err := ChooseProtocol(ProtocolAuto, getenv, false)
			if err != nil || got != ProtocolOff {
				t.Errorf("auto in %s inside %s = %q, %v; want off", innerName, outerName, got, err)
			}
			// A multiplexer does not pass an image on, and tmux takes a
			// Kitty sequence for its pane title: naming a protocol there
			// must not send one.
			for _, forced := range []string{ProtocolKitty, ProtocolITerm, " Kitty "} {
				got, err := ChooseProtocol(forced, getenv, false)
				if err != nil || got != ProtocolOff {
					t.Errorf("--images %s in %s inside %s = %q, %v; want off", forced, innerName, outerName, got, err)
				}
				if !NamedInMultiplexer(forced, getenv) {
					t.Errorf("--images %s in %s inside %s is not reported as named in a multiplexer", forced, innerName, outerName)
				}
			}
			for _, flag := range []string{ProtocolAuto, ProtocolOff, "", "sixel"} {
				if NamedInMultiplexer(flag, getenv) {
					t.Errorf("--images %q in %s names no protocol", flag, innerName)
				}
			}
		}
		for _, forced := range []string{ProtocolKitty, ProtocolITerm} {
			if NamedInMultiplexer(forced, direct) {
				t.Errorf("--images %s in %s alone is reported as inside a multiplexer", forced, outerName)
			}
		}
	}
}

// A terminal started from another inherits KITTY_WINDOW_ID, WEZTERM_PANE, and
// LC_TERMINAL. Auto must go by what the terminal on screen says of itself, or
// it reserves rows for a replay that terminal never draws.
func TestChooseProtocolIgnoresAnInheritedTerminal(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"kitty", map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1"}, ProtocolKitty},
		{"kitty with its own TERM setting", map[string]string{"TERM": "xterm-256color", "KITTY_WINDOW_ID": "1"}, ProtocolKitty},
		{"kitty across ssh", map[string]string{"TERM": "xterm-kitty"}, ProtocolKitty},
		{"ghostty", map[string]string{"TERM": "xterm-ghostty", "TERM_PROGRAM": "ghostty"}, ProtocolKitty},
		{"wezterm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "WezTerm", "WEZTERM_PANE": "0"}, ProtocolKitty},
		{"iterm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "iTerm.app", "LC_TERMINAL": "iTerm2"}, ProtocolITerm},
		{"iterm across ssh", map[string]string{"TERM": "xterm-256color", "LC_TERMINAL": "iTerm2"}, ProtocolITerm},

		{"vscode started from iterm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "vscode", "LC_TERMINAL": "iTerm2"}, ProtocolOff},
		{"vscode started from kitty", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "vscode", "KITTY_WINDOW_ID": "3"}, ProtocolOff},
		{"apple terminal started from iterm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "Apple_Terminal", "LC_TERMINAL": "iTerm2"}, ProtocolOff},
		{"zed started from wezterm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "zed", "WEZTERM_PANE": "0"}, ProtocolOff},
		{"kitty started from iterm", map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1", "TERM_PROGRAM": "iTerm.app", "LC_TERMINAL": "iTerm2"}, ProtocolKitty},
		{"neovim terminal in kitty", map[string]string{"TERM": "xterm-256color", "KITTY_WINDOW_ID": "1", "NVIM": "/tmp/nvim.sock"}, ProtocolOff},
		{"vim terminal in kitty", map[string]string{"TERM": "xterm-256color", "KITTY_WINDOW_ID": "1", "VIM_TERMINAL": "901"}, ProtocolOff},
		{"emacs terminal in iterm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "iTerm.app", "INSIDE_EMACS": "29.1,vterm"}, ProtocolOff},
		{"a terminal that says nothing", map[string]string{"TERM": "xterm-256color"}, ProtocolOff},

		// These set no TERM_PROGRAM of their own, so they keep the one they
		// were started under along with the other inherited variables.
		{"alacritty started from kitty", map[string]string{"TERM": "alacritty", "KITTY_WINDOW_ID": "1"}, ProtocolOff},
		{"alacritty started from iterm", map[string]string{"TERM": "alacritty-direct", "TERM_PROGRAM": "iTerm.app", "LC_TERMINAL": "iTerm2"}, ProtocolOff},
		{"foot started from wezterm", map[string]string{"TERM": "foot", "TERM_PROGRAM": "WezTerm", "WEZTERM_PANE": "0"}, ProtocolOff},
		{"jetbrains terminal, ide started from iterm", map[string]string{"TERM": "xterm-256color", "TERMINAL_EMULATOR": "JetBrains-JediTerm", "TERM_PROGRAM": "iTerm.app", "LC_TERMINAL": "iTerm2"}, ProtocolOff},
		{"jetbrains terminal, ide started from kitty", map[string]string{"TERM": "xterm-256color", "TERMINAL_EMULATOR": "JetBrains-JediTerm", "KITTY_WINDOW_ID": "2"}, ProtocolOff},
		{"linux console", map[string]string{"TERM": "linux", "LC_TERMINAL": "iTerm2"}, ProtocolOff},
		{"kitty started from a jetbrains terminal", map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1", "TERMINAL_EMULATOR": "JetBrains-JediTerm"}, ProtocolKitty},
	}
	for _, tc := range cases {
		getenv := func(key string) string { return tc.env[key] }
		if got, err := ChooseProtocol(ProtocolAuto, getenv, false); err != nil || got != tc.want {
			t.Errorf("auto in %s = %q, %v; want %q", tc.name, got, err, tc.want)
		}
		for _, forced := range []string{ProtocolKitty, ProtocolITerm} {
			if got, err := ChooseProtocol(forced, getenv, false); err != nil || got != forced {
				t.Errorf("--images %s in %s = %q, %v", forced, tc.name, got, err)
			}
		}
	}
}

// Plain output draws no replay. Naming a protocol does not change that: the
// command's help says so, and a bad name is still refused.
func TestChooseProtocolPlainOutputDrawsNoReplay(t *testing.T) {
	kitty := func(key string) string {
		return map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1"}[key]
	}
	for _, flag := range []string{ProtocolAuto, ProtocolKitty, ProtocolITerm, ProtocolOff, ""} {
		if got, err := ChooseProtocol(flag, kitty, true); err != nil || got != ProtocolOff {
			t.Errorf("--images %q with plain output = %q, %v; want off", flag, got, err)
		}
	}
	if _, err := ChooseProtocol("sixel", kitty, true); err == nil {
		t.Error("plain output accepted an unknown --images value")
	}
}
