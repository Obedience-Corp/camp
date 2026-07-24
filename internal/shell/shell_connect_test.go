package shell

import (
	"strings"
	"testing"
)

func TestSwitchBranchEvalsShellConnect(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		connect string
	}{
		{"zsh", generateZsh(), `command camp switch "$@" --shell-connect`},
		{"bash", generateBash(), `command camp switch "$@" --shell-connect`},
		{"fish", generateFish(), `command camp switch $rest --shell-connect`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.output, tc.connect) {
				t.Errorf("%s switch branch does not call --shell-connect: missing %q", tc.name, tc.connect)
			}
			if !strings.Contains(tc.output, `eval "$line"`) {
				t.Errorf("%s does not eval the --shell-connect line", tc.name)
			}
			// The scriptable-mode passthrough guard (--help/--json/--print) must survive.
			if !strings.Contains(tc.output, "--print") {
				t.Errorf("%s lost the --print passthrough guard", tc.name)
			}
			// The old local-only --print+cd switch path must be gone.
			if strings.Contains(tc.output, `command camp switch "$@" --print`) {
				t.Errorf("%s still uses the old --print switch path", tc.name)
			}
		})
	}
}

func TestListBranchHandlesSSHHopMarker(t *testing.T) {
	cases := []struct {
		name   string
		output string
		start  string
		end    string
		// Distinct markers that must appear in the list arm for remote go.
		needles []string
	}{
		{
			name:   "zsh",
			output: generateZsh(),
			start:  "list|ls)",
			end:    "festivals)",
			needles: []string{
				"ssh-hop:",
				`sel="${dest#ssh-hop:}"`,
				`command camp switch "$sel" --shell-connect`,
				`cd "$dest"`,
			},
		},
		{
			name:   "bash",
			output: generateBash(),
			start:  "list|ls)",
			end:    "festivals)",
			needles: []string{
				"ssh-hop:",
				`sel="${dest#ssh-hop:}"`,
				`command camp switch "$sel" --shell-connect`,
				`cd "$dest"`,
			},
		},
		{
			name:   "fish",
			output: generateFish(),
			start:  "case list ls",
			end:    "case festivals",
			needles: []string{
				"ssh-hop:",
				`string replace -r '^ssh-hop:'`,
				"command camp switch $sel --shell-connect",
				`cd "$dest"`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Scope to the list arm so switch-branch shell-connect does not
			// falsely satisfy the markers.
			section := shellWrapperSection(t, tc.output, tc.start, tc.end)
			for _, n := range tc.needles {
				if !strings.Contains(section, n) {
					t.Errorf("%s list arm missing %q", tc.name, n)
				}
			}
		})
	}
}
