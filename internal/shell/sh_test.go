package shell

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// bashOnlyConstructs are syntaxes a POSIX shell cannot parse. The shared
// posix_core blocks and the sh script must contain none of them: a single one
// aborts the whole eval, which is how `eval "$(camp shell-init bash)"` under
// busybox ash produced `syntax error: unexpected "(" (expecting "fi")` and left
// the user with no integration at all.
var bashOnlyConstructs = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"array assignment", regexp.MustCompile(`(^|\s)(local\s+|export\s+|declare\s+|readonly\s+)?[A-Za-z_][A-Za-z0-9_]*=\(`)},
	{"[[ test", regexp.MustCompile(`\[\[`)},
	{"&> redirect", regexp.MustCompile(`&>`)},
	{"complete builtin", regexp.MustCompile(`^\s*complete\s`)},
	{"compgen builtin", regexp.MustCompile(`\bcompgen\b`)},
	{"shopt builtin", regexp.MustCompile(`^\s*shopt\s`)},
	{"function keyword", regexp.MustCompile(`^\s*function\s`)},
	{"bash variable", regexp.MustCompile(`\$\{?(BASH_|COMP_|COMPREPLY)`)},
	{"substring expansion", regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*:\d`)},
}

// The sh script is the one camp hands to shells with no bash. Anything
// bash-only in it is a syntax error for the user, not a degraded feature.
func TestShScriptHasNoBashOnlySyntax(t *testing.T) {
	assertNoBashOnlySyntax(t, generateSh())
}

// The posix_core blocks are shared with the bash script, so a bash-ism added
// there compiles fine and breaks only sh users. This is the regression guard
// for that: it fails at the source, not at the far end.
func TestPosixCoreHasNoBashOnlySyntax(t *testing.T) {
	raw, err := templateFS.ReadFile("templates/posix_core.sh.tmpl")
	if err != nil {
		t.Fatalf("read posix_core template: %v", err)
	}
	assertNoBashOnlySyntax(t, string(raw))
}

func assertNoBashOnlySyntax(t *testing.T, script string) {
	t.Helper()
	for i, line := range strings.Split(stripTemplateComments(script), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue // comments describe the rule; they do not execute
		}
		for _, c := range bashOnlyConstructs {
			if c.pattern.MatchString(line) {
				t.Errorf("line %d uses %s, which POSIX sh cannot parse:\n  %s", i+1, c.name, line)
			}
		}
	}
}

// A static scan proves no known bash-ism is present; a real POSIX shell proves
// the script actually parses. Both matter, because the scan only knows the
// constructs it was taught. Skips where no POSIX shell is installed rather than
// silently passing (macOS /bin/sh is bash in POSIX mode and would accept the
// very syntax this guards against, so it is deliberately not used).
func TestShScriptParsesUnderRealPosixShell(t *testing.T) {
	var sh string
	for _, candidate := range []string{"dash", "busybox", "ash"} {
		if path, err := exec.LookPath(candidate); err == nil {
			sh = path
			break
		}
	}
	if sh == "" {
		t.Skip("no dash/busybox/ash on PATH; the container lane covers this")
	}

	args := []string{"-n"}
	if strings.HasSuffix(sh, "busybox") {
		args = append([]string{"sh"}, args...)
	}
	cmd := exec.Command(sh, args...)
	cmd.Stdin = strings.NewReader(generateSh())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s rejected the sh script: %v\n%s", sh, err, out)
	}
}

// sh is a first-class supported shell, not an alias that happens to render.
func TestShIsSupported(t *testing.T) {
	if !IsSupported("sh") {
		t.Error(`IsSupported("sh") = false, want true`)
	}
	out, err := Generate("sh")
	if err != nil {
		t.Fatalf("Generate(sh) error = %v", err)
	}
	for _, want := range []string{"camp() {", "cgo() {", "csw() {", "cr() {", "corg() {"} {
		if !strings.Contains(out, want) {
			t.Errorf("sh script missing %q", want)
		}
	}
	if strings.Contains(out, "_camp_complete") {
		t.Error("sh script must not carry bash completion functions")
	}
}

// The bash and sh scripts share posix_core, so the wrapper the user drives has
// to be the same one in both. A copy that drifts is the failure this prevents.
func TestShAndBashShareTheSameWrapper(t *testing.T) {
	shScript, bashScript := generateSh(), generateBash()
	// The camp() wrapper runs from its opening line to the cgo() shorthand that
	// follows it in posix_core.
	extract := func(s string) string {
		start := strings.Index(s, "# Wrap camp binary")
		end := strings.Index(s, "# Shorthand for camp go")
		if start < 0 || end < 0 || end < start {
			t.Fatalf("could not locate the wrapper block (start=%d end=%d)", start, end)
		}
		return s[start:end]
	}
	if extract(shScript) != extract(bashScript) {
		t.Error("sh and bash wrappers have drifted; they must both come from posix_core")
	}
}

// stripTemplateComments blanks {{/* ... */}} spans so prose describing the
// banned constructs is not itself read as a violation, while keeping line
// numbers intact for the failure message.
func stripTemplateComments(script string) string {
	var b strings.Builder
	rest := script
	for {
		start := strings.Index(rest, "{{/*")
		if start < 0 {
			b.WriteString(rest)
			return b.String()
		}
		end := strings.Index(rest[start:], "*/}}")
		if end < 0 {
			b.WriteString(rest)
			return b.String()
		}
		end += start + len("*/}}")
		b.WriteString(rest[:start])
		b.WriteString(strings.Repeat("\n", strings.Count(rest[start:end], "\n")))
		rest = rest[end:]
	}
}
