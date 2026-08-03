package shell

import (
	"strings"
	"testing"
)

// The machine arm exists so the fleet screen's enter key can finish a hop: the
// TUI writes ssh-hop:<machine>:<campaign> and the wrapper turns that into the
// `camp switch --shell-connect` line it evals. A subprocess cannot replace its
// parent shell, so without this arm the key is inert.
//
// Every shell gets the same three guarantees, asserted per shell because the
// three templates are hand-maintained rather than generated from one source:
// subcommands pass through untouched, the hop is completed via switch, and the
// arm never cd's (a machine row is never a local path).
func TestMachineArm_AllShells(t *testing.T) {
	for _, sh := range []struct {
		name        string
		output      string
		start, end  string
		passthrough string
		hopVia      string
		tmpFile     string
	}{
		{
			name:        "zsh",
			output:      generateZsh(),
			start:       "    machine)",
			end:         "    festivals)",
			passthrough: `command camp machine "$@"`,
			hopVia:      `line=$(command camp switch "$sel" --shell-connect)`,
			tmpFile:     "camp-machine.XXXXXX",
		},
		{
			name:        "bash",
			output:      generateBash(),
			start:       "    machine)",
			end:         "    festivals)",
			passthrough: `command camp machine "$@"`,
			hopVia:      `line=$(command camp switch "$sel" --shell-connect)`,
			tmpFile:     "camp-machine.XXXXXX",
		},
		{
			name:        "fish",
			output:      generateFish(),
			start:       "        case machine",
			end:         "        case festivals",
			passthrough: "command camp machine $rest",
			hopVia:      "command camp switch $sel --shell-connect",
			tmpFile:     "camp-machine.XXXXXX",
		},
	} {
		t.Run(sh.name, func(t *testing.T) {
			section := shellWrapperSection(t, sh.output, sh.start, sh.end)

			for _, check := range []struct{ name, content string }{
				{"path output seam", "--path-output"},
				{"own temp file", sh.tmpFile},
				{"subcommand passthrough", sh.passthrough},
				{"hop completed via switch", sh.hopVia},
				{"ssh-hop marker", "ssh-hop:"},
			} {
				if !strings.Contains(section, check.content) {
					t.Errorf("%s machine arm missing %s: %q", sh.name, check.name, check.content)
				}
			}

			// A machine row resolves to a remote campaign root the far side owns,
			// never to a local directory. A cd here would mean the arm had
			// silently grown a second, wrong meaning for the payload.
			for _, forbidden := range []string{`cd "$dest"`, `cd "$root/$dest"`, "cd $dest"} {
				if strings.Contains(section, forbidden) {
					t.Errorf("%s machine arm must not cd: found %q", sh.name, forbidden)
				}
			}
		})
	}
}

// The fleet screen takes no positional argument, so a bare first word is always
// a subcommand and must reach the binary untouched. Without this guard
// `camp machine list` would be handed --path-output, which it does not define.
func TestMachineArm_BareWordSubcommandGuard(t *testing.T) {
	for _, sh := range []struct {
		name       string
		output     string
		start, end string
		guard      string
	}{
		{"zsh", generateZsh(), "    machine)", "    festivals)", `""|-*) ;;`},
		{"bash", generateBash(), "    machine)", "    festivals)", `""|-*) ;;`},
		{"fish", generateFish(), "        case machine", "        case festivals",
			`if test (count $rest) -gt 0; and not string match -qr -- '^-' $rest[1]`},
	} {
		t.Run(sh.name, func(t *testing.T) {
			section := shellWrapperSection(t, sh.output, sh.start, sh.end)
			if !strings.Contains(section, sh.guard) {
				t.Errorf("%s machine arm missing bare-word subcommand guard: %q", sh.name, sh.guard)
			}
		})
	}
}
