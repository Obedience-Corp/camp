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
		statusCapture:      "status=$?",
		statusCheck:        `elif [[ "$status" -ne 0 ]]; then`,
		statusReturn:       "return $status",
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
