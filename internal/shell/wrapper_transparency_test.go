package shell

import (
	"strings"
	"testing"
)

func TestGenerateBash_GoSwitchWrapperTransparency(t *testing.T) {
	output := generateBash()
	section := shellWrapperSection(t, output, "    switch|sw)", "    workitem|wi|workitems)")

	assertShellWrapperTransparency(t, section, shellWrapperExpectations{
		noSuppressedGo:     `command camp go "$@" --print 2>/dev/null`,
		noSuppressedRoot:   `command camp go --print 2>/dev/null`,
		noSuppressedSwitch: `command camp switch "$@" --print 2>/dev/null`,
		passthrough:        "--help|-h|--json|--json=*|--print|--print=*",
		statusCapture:      "status=$?",
		statusCheck:        `elif [ "$status" -ne 0 ]; then`,
		statusReturn:       `return "$status"`,
	})
}

func TestGenerateZsh_GoSwitchWrapperTransparency(t *testing.T) {
	output := generateZsh()
	section := shellWrapperSection(t, output, "    switch|sw)", "    workitem|wi|workitems)")

	assertShellWrapperTransparency(t, section, shellWrapperExpectations{
		noSuppressedGo:     `command camp go "$@" --print 2>/dev/null`,
		noSuppressedRoot:   `command camp go --print 2>/dev/null`,
		noSuppressedSwitch: `command camp switch "$@" --print 2>/dev/null`,
		passthrough:        "--help|-h|--json|--json=*|--print|--print=*",
		statusCapture:      "cmd_status=$?",
		statusCheck:        `elif [[ "$cmd_status" -ne 0 ]]; then`,
		statusReturn:       "return $cmd_status",
	})
}

func TestGenerateFish_GoSwitchWrapperTransparency(t *testing.T) {
	output := generateFish()
	section := shellWrapperSection(t, output, "        case switch sw", "        case workitem wi workitems")

	assertShellWrapperTransparency(t, section, shellWrapperExpectations{
		noSuppressedGo:     "command camp go $rest --print 2>/dev/null",
		noSuppressedRoot:   "command camp go --print 2>/dev/null",
		noSuppressedSwitch: "command camp switch $rest --print 2>/dev/null",
		passthrough:        "case --help -h --json '--json=*' --print '--print=*'",
		statusCapture:      "set -l cmd_status $status",
		statusCheck:        `else if test "$cmd_status" -ne 0`,
		statusReturn:       "return $cmd_status",
	})
}

type shellWrapperExpectations struct {
	noSuppressedGo     string
	noSuppressedRoot   string
	noSuppressedSwitch string
	passthrough        string
	statusCapture      string
	statusCheck        string
	statusReturn       string
}

func assertShellWrapperTransparency(t *testing.T, section string, expect shellWrapperExpectations) {
	t.Helper()

	for _, forbidden := range []string{expect.noSuppressedGo, expect.noSuppressedRoot, expect.noSuppressedSwitch} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("go/switch wrapper still suppresses stderr with %q", forbidden)
		}
	}
	for _, required := range []string{expect.passthrough, expect.statusCapture, expect.statusCheck, expect.statusReturn} {
		if !strings.Contains(section, required) {
			t.Fatalf("go/switch wrapper missing %q in:\n%s", required, section)
		}
	}
}

// workitemArmExpectations describes the per-shell tokens that prove the
// workitem wrapper arm passes subcommands through verbatim while still
// intercepting the interactive dashboard.
type workitemArmExpectations struct {
	subcommandGuard string // construct that passes a bare-word subcommand through
	passthroughCall string // the verbatim passthrough invocation
	listPassthrough string // --list joined the dashboard output-mode flags
	pathOutput      string // the dashboard --path-output interception, still present
}

func assertWorkitemArmPassesSubcommands(t *testing.T, section string, e workitemArmExpectations) {
	t.Helper()

	for _, required := range []string{e.subcommandGuard, e.passthroughCall, e.listPassthrough, e.pathOutput} {
		if !strings.Contains(section, required) {
			t.Fatalf("workitem arm missing %q in:\n%s", required, section)
		}
	}
	// The subcommand guard must run BEFORE the --path-output interception; if it
	// did not, a subcommand invocation (camp workitem resolve/id/stage/...) would
	// still get --path-output appended and die with exit 2.
	if strings.Index(section, e.subcommandGuard) >= strings.Index(section, e.pathOutput) {
		t.Fatalf("workitem arm applies --path-output before guarding subcommands:\n%s", section)
	}
}

func TestGenerateZsh_WorkitemArmPassesSubcommands(t *testing.T) {
	section := shellWrapperSection(t, generateZsh(), "    workitem|wi|workitems)", "    list|ls)")
	assertWorkitemArmPassesSubcommands(t, section, workitemArmExpectations{
		subcommandGuard: `case "${1:-}" in`,
		passthroughCall: `command camp workitem "$@"`,
		listPassthrough: `--print|--print=*|--list)`,
		pathOutput:      `--path-output "$tmp"`,
	})
}

func TestGenerateBash_WorkitemArmPassesSubcommands(t *testing.T) {
	section := shellWrapperSection(t, generateBash(), "    workitem|wi|workitems)", "    list|ls)")
	assertWorkitemArmPassesSubcommands(t, section, workitemArmExpectations{
		subcommandGuard: `case "${1:-}" in`,
		passthroughCall: `command camp workitem "$@"`,
		listPassthrough: `--print|--print=*|--list)`,
		pathOutput:      `--path-output "$tmp"`,
	})
}

func TestGenerateFish_WorkitemArmPassesSubcommands(t *testing.T) {
	section := shellWrapperSection(t, generateFish(), "        case workitem wi workitems", "        case list ls")
	assertWorkitemArmPassesSubcommands(t, section, workitemArmExpectations{
		subcommandGuard: `not string match -qr -- '^-' $rest[1]`,
		passthroughCall: `command camp workitem $rest`,
		listPassthrough: `--print '--print=*' --list`,
		pathOutput:      `--path-output "$tmp"`,
	})
}

func shellWrapperSection(t *testing.T, output, startMarker, endMarker string) string {
	t.Helper()

	start := strings.Index(output, startMarker)
	if start < 0 {
		t.Fatalf("missing shell wrapper start marker %q", startMarker)
	}
	end := strings.Index(output[start:], endMarker)
	if end < 0 {
		t.Fatalf("missing shell wrapper end marker %q", endMarker)
	}
	return output[start : start+end]
}
