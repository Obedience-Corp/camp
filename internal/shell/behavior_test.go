package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// behaviorTest describes a single shell behavior verification.
type behaviorTest struct {
	name   string
	script string // shell code executed after sourcing the init script
	check  func(t *testing.T, stdout, stderr string, exitCode int)
}

// ---------- Bash behavior tests ----------

func TestBashBehavior_CampWrapperGo(t *testing.T) {
	requireShell(t, "bash")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "camp go invokes command camp with --print and cd's",
			// Stub: "command camp" returns the target dir when called with "go foo --print"
			script: stubCamp(target, "bash") + `
camp go foo
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s, got stdout:\n%s", target, stdout)
				}
			},
		},
		{
			name: "camp go with no args invokes --print and cd's",
			script: stubCamp(target, "bash") + `
camp go
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s, got stdout:\n%s", target, stdout)
				}
			},
		},
		{
			name: "camp non-nav subcommand passes through to binary",
			script: stubCamp(target, "bash") + `
camp version
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough, got stdout:\n%s", stdout)
				}
			},
		},
	}

	runBashBehavior(t, tests)
}

func TestBashBehavior_CgoWrapper(t *testing.T) {
	requireShell(t, "bash")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "cgo delegates to camp go and cd's",
			script: stubCamp(target, "bash") + `
cgo foo
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s, got stdout:\n%s", target, stdout)
				}
			},
		},
	}

	runBashBehavior(t, tests)
}

func TestBashBehavior_AliasWrappers(t *testing.T) {
	requireShell(t, "bash")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "cr delegates to camp run",
			script: stubCamp(target, "bash") + `
cr echo hello
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				// cr calls camp run, which goes through the passthrough path
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough for cr, got stdout:\n%s", stdout)
				}
			},
		},
		{
			name: "csw delegates to camp switch",
			script: stubCamp(target, "bash") + `
csw mycamp
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s after csw, got stdout:\n%s", target, stdout)
				}
			},
		},
		{
			name: "cint delegates to camp intent add",
			script: stubCamp(target, "bash") + `
cint "my idea"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough for cint, got stdout:\n%s", stdout)
				}
			},
		},
		{
			name: "cie delegates to camp intent explore",
			script: stubCamp(target, "bash") + `
cie
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough for cie, got stdout:\n%s", stdout)
				}
			},
		},
	}

	runBashBehavior(t, tests)
}

func TestBashBehavior_CompletionFunction(t *testing.T) {
	requireShell(t, "bash")

	tests := []behaviorTest{
		{
			name: "cgo completion function is registered",
			script: `
complete -p cgo 2>/dev/null && echo "CGO_COMPLETION_REGISTERED"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if !strings.Contains(stdout, "CGO_COMPLETION_REGISTERED") {
					t.Errorf("cgo completion not registered, stdout:\n%s", stdout)
				}
			},
		},
		{
			name: "camp completion function is registered",
			script: `
complete -p camp 2>/dev/null && echo "CAMP_COMPLETION_REGISTERED"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if !strings.Contains(stdout, "CAMP_COMPLETION_REGISTERED") {
					t.Errorf("camp completion not registered, stdout:\n%s", stdout)
				}
			},
		},
	}

	runBashBehavior(t, tests)
}

// ---------- Zsh behavior tests ----------

func TestZshBehavior_CampWrapperGo(t *testing.T) {
	requireShell(t, "zsh")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "camp go invokes command camp with --print and cd's",
			script: stubCamp(target, "zsh") + `
camp go foo
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s, got stdout:\n%s", target, stdout)
				}
			},
		},
		{
			name: "camp go with no args invokes --print and cd's",
			script: stubCamp(target, "zsh") + `
camp go
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s, got stdout:\n%s", target, stdout)
				}
			},
		},
		{
			name: "camp non-nav subcommand passes through to binary",
			script: stubCamp(target, "zsh") + `
camp version
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough, got stdout:\n%s", stdout)
				}
			},
		},
	}

	runZshBehavior(t, tests)
}

func TestZshBehavior_CgoWrapper(t *testing.T) {
	requireShell(t, "zsh")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "cgo delegates to camp go and cd's",
			script: stubCamp(target, "zsh") + `
cgo foo
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s, got stdout:\n%s", target, stdout)
				}
			},
		},
	}

	runZshBehavior(t, tests)
}

func TestZshBehavior_AliasWrappers(t *testing.T) {
	requireShell(t, "zsh")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "cr delegates to camp run",
			script: stubCamp(target, "zsh") + `
cr echo hello
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough for cr, got stdout:\n%s", stdout)
				}
			},
		},
		{
			name: "csw delegates to camp switch",
			script: stubCamp(target, "zsh") + `
csw mycamp
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s after csw, got stdout:\n%s", target, stdout)
				}
			},
		},
		{
			name: "cint delegates to camp intent add",
			script: stubCamp(target, "zsh") + `
cint "my idea"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough for cint, got stdout:\n%s", stdout)
				}
			},
		},
		{
			name: "cie delegates to camp intent explore",
			script: stubCamp(target, "zsh") + `
cie
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough for cie, got stdout:\n%s", stdout)
				}
			},
		},
	}

	runZshBehavior(t, tests)
}

func TestZshBehavior_CompdefRegistered(t *testing.T) {
	requireShell(t, "zsh")

	// Zsh compdef registration happens at source time. We verify the
	// generated script contains the compdef directives (syntax test covers
	// the rest). A more complete compdef test would need a full zsh
	// completion system init which is expensive.
	output := generateZsh()
	if !strings.Contains(output, "compdef _cgo cgo") {
		t.Error("zsh init missing compdef _cgo cgo")
	}
	if !strings.Contains(output, "compdef _camp camp") {
		t.Error("zsh init missing compdef _camp camp")
	}
	if !strings.Contains(output, "compdef _csw csw") {
		t.Error("zsh init missing compdef _csw csw")
	}
}

// ---------- Fish behavior tests ----------

func TestFishBehavior_CampWrapperGo(t *testing.T) {
	requireShell(t, "fish")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "camp go invokes command camp with --print and cd's",
			script: stubCamp(target, "fish") + `
camp go foo
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s, got stdout:\n%s", target, stdout)
				}
			},
		},
		{
			name: "camp non-nav subcommand passes through to binary",
			script: stubCamp(target, "fish") + `
camp version
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough, got stdout:\n%s", stdout)
				}
			},
		},
	}

	runFishBehavior(t, tests)
}

func TestFishBehavior_CgoWrapper(t *testing.T) {
	requireShell(t, "fish")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "cgo delegates to camp go and cd's",
			script: stubCamp(target, "fish") + `
cgo foo
echo "PWD=$PWD"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "PWD="+target) {
					t.Errorf("expected PWD=%s, got stdout:\n%s", target, stdout)
				}
			},
		},
	}

	runFishBehavior(t, tests)
}

func TestFishBehavior_AliasWrappers(t *testing.T) {
	requireShell(t, "fish")

	target := t.TempDir()

	tests := []behaviorTest{
		{
			name: "cr delegates to camp run",
			script: stubCamp(target, "fish") + `
cr echo hello
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough for cr, got stdout:\n%s", stdout)
				}
			},
		},
		{
			name: "cint delegates to camp intent add",
			script: stubCamp(target, "fish") + `
cint "my idea"
`,
			check: func(t *testing.T, stdout, stderr string, exitCode int) {
				t.Helper()
				if exitCode != 0 {
					t.Fatalf("exit code %d, stderr: %s", exitCode, stderr)
				}
				if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
					t.Errorf("expected passthrough for cint, got stdout:\n%s", stdout)
				}
			},
		},
	}

	runFishBehavior(t, tests)
}

// ---------- Syntax validation tests ----------

func TestBashSyntaxValidation(t *testing.T) {
	requireShell(t, "bash")
	validateShellSyntax(t, "bash", generateBash())
}

func TestZshSyntaxValidation(t *testing.T) {
	requireShell(t, "zsh")
	validateShellSyntax(t, "zsh", generateZsh())
}

func TestFishSyntaxValidation(t *testing.T) {
	requireShell(t, "fish")
	validateShellSyntax(t, "fish", generateFish())
}

// ---------- Helpers ----------

// requireShell skips the test if the named shell binary is not in PATH.
func requireShell(t *testing.T, shell string) {
	t.Helper()
	if _, err := exec.LookPath(shell); err != nil {
		t.Skipf("%s not available", shell)
	}
}

// validateShellSyntax writes the script to a temp file and runs <shell> -n to
// check for syntax errors.
func validateShellSyntax(t *testing.T, shell, script string) {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "camp-*.sh")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(script); err != nil {
		t.Fatalf("write script: %v", err)
	}
	f.Close()

	cmd := exec.Command(shell, "-n", f.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("%s syntax error:\n%s\n%v", shell, out, err)
	}
}

// stubCamp returns shell code that creates a fake "camp" binary in a temp
// directory and prepends it to PATH. The stub handles:
//   - go/switch with --print: prints the target directory
//   - everything else: prints STUB_PASSTHROUGH
func stubCamp(targetDir, shell string) string {
	// All three shell families can use the same stub script since
	// the stub is a standalone #!/bin/sh executable, not sourced code.
	return `
# Create a stub camp binary
_stub_dir=$(mktemp -d)
cat > "$_stub_dir/camp" << 'STUBEOF'
#!/bin/sh
# Minimal camp stub for behavior tests
for arg in "$@"; do
  if [ "$arg" = "--print" ]; then
    echo "` + targetDir + `"
    exit 0
  fi
done
echo "STUB_PASSTHROUGH"
STUBEOF
chmod +x "$_stub_dir/camp"
export PATH="$_stub_dir:$PATH"
`
}

// runBashBehavior generates the bash init script, writes a test harness that
// sources it, then executes each behavior test inside bash.
func runBashBehavior(t *testing.T, tests []behaviorTest) {
	t.Helper()

	initScript := generateBash()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Write the init script
			initPath := filepath.Join(dir, "camp-init.sh")
			if err := os.WriteFile(initPath, []byte(initScript), 0644); err != nil {
				t.Fatalf("write init script: %v", err)
			}

			// Build the test harness
			harness := tt.script
			fullScript := "source " + initPath + "\n" + harness

			scriptPath := filepath.Join(dir, "test.sh")
			if err := os.WriteFile(scriptPath, []byte(fullScript), 0644); err != nil {
				t.Fatalf("write test script: %v", err)
			}

			stdout, stderr, exitCode := runShell(t, "bash", scriptPath)
			tt.check(t, stdout, stderr, exitCode)
		})
	}
}

// runZshBehavior generates the zsh init script, writes a test harness that
// sources it, then executes each behavior test inside zsh.
func runZshBehavior(t *testing.T, tests []behaviorTest) {
	t.Helper()

	initScript := generateZsh()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			initPath := filepath.Join(dir, "camp-init.zsh")
			if err := os.WriteFile(initPath, []byte(initScript), 0644); err != nil {
				t.Fatalf("write init script: %v", err)
			}

			// Zsh needs NO_RCS to avoid loading user config, and we disable
			// the compinit requirement for the test harness.
			harness := tt.script
			fullScript := "emulate -R zsh\n" +
				"autoload -Uz compinit && compinit -u 2>/dev/null\n" +
				"source " + initPath + "\n" + harness

			scriptPath := filepath.Join(dir, "test.zsh")
			if err := os.WriteFile(scriptPath, []byte(fullScript), 0644); err != nil {
				t.Fatalf("write test script: %v", err)
			}

			stdout, stderr, exitCode := runShell(t, "zsh", "--no-rcs", scriptPath)
			tt.check(t, stdout, stderr, exitCode)
		})
	}
}

// runFishBehavior generates the fish init script, writes a test harness that
// sources it, then executes each behavior test inside fish.
func runFishBehavior(t *testing.T, tests []behaviorTest) {
	t.Helper()

	initScript := generateFish()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			initPath := filepath.Join(dir, "camp-init.fish")
			if err := os.WriteFile(initPath, []byte(initScript), 0644); err != nil {
				t.Fatalf("write init script: %v", err)
			}

			harness := tt.script
			fullScript := "source " + initPath + "\n" + harness

			scriptPath := filepath.Join(dir, "test.fish")
			if err := os.WriteFile(scriptPath, []byte(fullScript), 0644); err != nil {
				t.Fatalf("write test script: %v", err)
			}

			stdout, stderr, exitCode := runShell(t, "fish", "--no-config", scriptPath)
			tt.check(t, stdout, stderr, exitCode)
		})
	}
}

// runShell executes a script in the given shell and returns stdout, stderr,
// and the exit code.
func runShell(t *testing.T, shell string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	cmd := exec.Command(shell, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run %s: %v", shell, err)
		}
	}

	return outBuf.String(), errBuf.String(), exitCode
}
