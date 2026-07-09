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
