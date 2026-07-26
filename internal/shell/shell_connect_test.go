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

// TestSwitchBranchEvalsQuoted pins the one property that decides whether an
// embedded `export CAMP_HOP_ORIGIN='...'` survives the wrapper: the hop line
// must be eval'd as a single quoted word. Unquoted (`eval $line`) the shell
// word-splits the line before eval sees it, and a payload containing spaces or
// quotes would be re-split into the wrong argv, silently mangling the origin or
// the ssh options. Every dialect must quote.
func TestSwitchBranchEvalsQuoted(t *testing.T) {
	cases := []struct {
		name   string
		output string
	}{
		{"zsh", generateZsh()},
		{"bash", generateBash()},
		{"fish", generateFish()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.output, `eval "$line"`) {
				t.Errorf("%s must eval the hop line as one quoted word", tc.name)
			}
			if strings.Contains(tc.output, "eval $line") {
				t.Errorf("%s evals the hop line unquoted; an embedded export would be word-split", tc.name)
			}
		})
	}
}

// TestSwitchBranchPassthroughGuardDoesNotSwallowDash pins that a bare "-"
// reaches the binary. The guard list exists for scriptable flags
// (--help/--json/--print); if "-" were ever added to it, the hop-back gesture
// would silently become a non-hopping passthrough instead of a hop.
//
// The check tokenizes each guard line rather than substring-matching, because
// every flag in the list starts with "-" and a naive substring test matches
// them all.
func TestSwitchBranchPassthroughGuardDoesNotSwallowDash(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{"zsh", generateZsh()},
		{"bash", generateBash()},
		{"fish", generateFish()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, line := range strings.Split(tc.output, "\n") {
				if !strings.Contains(line, "--help") {
					continue
				}
				for _, tok := range strings.FieldsFunc(line, func(r rune) bool {
					return r == ' ' || r == '|' || r == '\t' || r == ')' || r == '\''
				}) {
					if tok == "-" {
						t.Errorf("%s passthrough guard matches a bare '-': %q", tc.name, strings.TrimSpace(line))
					}
				}
			}
		})
	}
}
