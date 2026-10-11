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
			for _, forced := range []string{ProtocolKitty, ProtocolITerm} {
				got, err := ChooseProtocol(forced, getenv, false)
				if err != nil || got != forced {
					t.Errorf("--images %s in %s inside %s = %q, %v", forced, innerName, outerName, got, err)
				}
			}
		}
	}
}
