//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

// installShells installs bash, zsh, and fish in the container.
// Called once per test function that needs shells; the shared container
// Reset() doesn't remove APK-installed packages so this is idempotent
// across subtests within the same top-level test.
func installShells(t *testing.T, tc *TestContainer) {
	t.Helper()
	output, exitCode, err := tc.ExecCommand("sh", "-c", "apk add --no-cache bash zsh fish")
	if err != nil {
		t.Fatalf("failed to install shells: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("apk add bash zsh fish failed (exit %d): %s", exitCode, output)
	}
}

// shellInitScript generates the camp shell-init script inside the container.
func shellInitScript(t *testing.T, tc *TestContainer, shell string) string {
	t.Helper()
	output, err := tc.RunCamp("shell-init", shell)
	if err != nil {
		t.Fatalf("camp shell-init %s failed: %v", shell, err)
	}
	return strings.TrimSpace(output)
}

// stubCampScriptPosix returns POSIX shell code (bash/zsh compatible) that creates
// a fake "camp" binary in a temp directory and prepends it to PATH. The stub handles:
//   - any invocation containing --print: prints the target directory
//   - everything else: prints STUB_PASSTHROUGH
func stubCampScriptPosix(targetDir string) string {
	return fmt.Sprintf(`
_stub_dir=$(mktemp -d)
cat > "$_stub_dir/camp" << 'STUBEOF'
#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "--print" ]; then
    echo "%s"
    exit 0
  fi
done
echo "STUB_PASSTHROUGH"
STUBEOF
chmod +x "$_stub_dir/camp"
export PATH="$_stub_dir:$PATH"
`, targetDir)
}

// stubCampScriptFish returns fish shell code that creates a fake "camp" binary
// in a temp directory and prepends it to PATH.
func stubCampScriptFish(targetDir string) string {
	return fmt.Sprintf(`
set -l _stub_dir (mktemp -d)
printf '#!/bin/sh\nfor arg in "$@"; do\n  if [ "$arg" = "--print" ]; then\n    echo "%s"\n    exit 0\n  fi\ndone\necho "STUB_PASSTHROUGH"\n' > $_stub_dir/camp
chmod +x $_stub_dir/camp
set -gx PATH $_stub_dir $PATH
`, targetDir)
}

// runBashScript assembles and runs a bash script inside the container.
// Order: stubSetup (puts camp in PATH) -> source init -> testCommands.
// The init script has a `command -v camp` guard that exits early if camp
// is not in PATH, so the stub must be set up before sourcing.
func runBashScript(t *testing.T, tc *TestContainer, initScript, stubSetup, testCommands string) (string, int) {
	t.Helper()

	if err := tc.WriteFile("/test/camp-init.sh", initScript); err != nil {
		t.Fatalf("write init script: %v", err)
	}

	fullScript := stubSetup + "\nsource /test/camp-init.sh\n" + testCommands
	if err := tc.WriteFile("/test/test.sh", fullScript); err != nil {
		t.Fatalf("write test script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("bash", "/test/test.sh")
	if err != nil {
		t.Fatalf("exec bash: %v", err)
	}
	return output, exitCode
}

// runBashScriptNoStub runs a bash script without stub setup (for completion tests
// that only need the init script sourced with the real camp binary in PATH).
func runBashScriptNoStub(t *testing.T, tc *TestContainer, initScript, testCommands string) (string, int) {
	t.Helper()
	// The real camp binary is at /camp; ensure it is in PATH for the guard.
	return runBashScript(t, tc, initScript, `export PATH="/camp-bin:$PATH"`, testCommands)
}

// runZshScript assembles and runs a zsh script inside the container.
// Order: zsh preamble -> stubSetup -> source init -> testCommands.
func runZshScript(t *testing.T, tc *TestContainer, initScript, stubSetup, testCommands string) (string, int) {
	t.Helper()

	if err := tc.WriteFile("/test/camp-init.zsh", initScript); err != nil {
		t.Fatalf("write init script: %v", err)
	}

	fullScript := "emulate -R zsh\n" +
		"autoload -Uz compinit && compinit -u 2>/dev/null\n" +
		stubSetup + "\nsource /test/camp-init.zsh\n" + testCommands
	if err := tc.WriteFile("/test/test.zsh", fullScript); err != nil {
		t.Fatalf("write test script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("zsh", "--no-rcs", "/test/test.zsh")
	if err != nil {
		t.Fatalf("exec zsh: %v", err)
	}
	return output, exitCode
}

// runFishScript assembles and runs a fish script inside the container.
// Order: stubSetup -> source init -> testCommands.
func runFishScript(t *testing.T, tc *TestContainer, initScript, stubSetup, testCommands string) (string, int) {
	t.Helper()

	if err := tc.WriteFile("/test/camp-init.fish", initScript); err != nil {
		t.Fatalf("write init script: %v", err)
	}

	fullScript := stubSetup + "\nsource /test/camp-init.fish\n" + testCommands
	if err := tc.WriteFile("/test/test.fish", fullScript); err != nil {
		t.Fatalf("write test script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("fish", "--no-config", "/test/test.fish")
	if err != nil {
		t.Fatalf("exec fish: %v", err)
	}
	return output, exitCode
}

// ensureCampInPath creates a symlink so that `command -v camp` succeeds
// in the container. The real camp binary is at /camp; we create /camp-bin/camp.
func ensureCampInPath(t *testing.T, tc *TestContainer) {
	t.Helper()
	_, exitCode, err := tc.ExecCommand("sh", "-c", "mkdir -p /camp-bin && ln -sf /camp /camp-bin/camp")
	if err != nil || exitCode != 0 {
		t.Fatalf("failed to symlink camp into PATH: %v (exit %d)", err, exitCode)
	}
}

// ---------- Syntax validation tests ----------

func TestShellInit_BashSyntaxValidation(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	initScript := shellInitScript(t, tc, "bash")
	if err := tc.WriteFile("/test/syntax-check.sh", initScript); err != nil {
		t.Fatalf("write script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("bash", "-n", "/test/syntax-check.sh")
	if err != nil {
		t.Fatalf("exec bash -n: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("bash syntax error (exit %d):\n%s", exitCode, output)
	}
}

func TestShellInit_ZshSyntaxValidation(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	initScript := shellInitScript(t, tc, "zsh")
	if err := tc.WriteFile("/test/syntax-check.zsh", initScript); err != nil {
		t.Fatalf("write script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("zsh", "-n", "/test/syntax-check.zsh")
	if err != nil {
		t.Fatalf("exec zsh -n: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("zsh syntax error (exit %d):\n%s", exitCode, output)
	}
}

func TestShellInit_FishSyntaxValidation(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	initScript := shellInitScript(t, tc, "fish")
	if err := tc.WriteFile("/test/syntax-check.fish", initScript); err != nil {
		t.Fatalf("write script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("fish", "-n", "/test/syntax-check.fish")
	if err != nil {
		t.Fatalf("exec fish -n: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("fish syntax error (exit %d):\n%s", exitCode, output)
	}
}

// ---------- Bash behavior tests ----------

func TestShellInit_BashCampWrapperGo(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/nav-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "bash")
	stub := stubCampScriptPosix(targetDir)

	t.Run("camp_go_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
camp go foo
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_go_no_args_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
camp go
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_non_nav_subcommand_passes_through", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
camp version
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough, got:\n%s", stdout)
		}
	})
}

func TestShellInit_BashCgoWrapper(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/cgo-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "bash")
	stub := stubCampScriptPosix(targetDir)

	stdout, exitCode := runBashScript(t, tc, initScript, stub, `
cgo foo
echo "PWD=$PWD"
`)
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "PWD="+targetDir) {
		t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
	}
}

func TestShellInit_BashAliasWrappers(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/alias-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "bash")
	stub := stubCampScriptPosix(targetDir)

	t.Run("cr_delegates_to_camp_run", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
cr echo hello
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cr, got:\n%s", stdout)
		}
	})

	t.Run("csw_delegates_to_camp_switch", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
csw mycamp
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s after csw, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("cint_delegates_to_camp_intent_add", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
cint "my idea"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cint, got:\n%s", stdout)
		}
	})

	t.Run("cie_delegates_to_camp_intent_explore", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
cie
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cie, got:\n%s", stdout)
		}
	})
}

func TestShellInit_BashCompletionFunction(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)
	ensureCampInPath(t, tc)

	initScript := shellInitScript(t, tc, "bash")

	t.Run("cgo_completion_registered", func(t *testing.T) {
		stdout, _ := runBashScriptNoStub(t, tc, initScript, `
complete -p cgo 2>/dev/null && echo "CGO_COMPLETION_REGISTERED"
`)
		if !strings.Contains(stdout, "CGO_COMPLETION_REGISTERED") {
			t.Errorf("cgo completion not registered, output:\n%s", stdout)
		}
	})

	t.Run("camp_completion_registered", func(t *testing.T) {
		stdout, _ := runBashScriptNoStub(t, tc, initScript, `
complete -p camp 2>/dev/null && echo "CAMP_COMPLETION_REGISTERED"
`)
		if !strings.Contains(stdout, "CAMP_COMPLETION_REGISTERED") {
			t.Errorf("camp completion not registered, output:\n%s", stdout)
		}
	})
}

// ---------- Zsh behavior tests ----------

func TestShellInit_ZshCampWrapperGo(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/zsh-nav-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "zsh")
	stub := stubCampScriptPosix(targetDir)

	t.Run("camp_go_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
camp go foo
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_go_no_args_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
camp go
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_non_nav_subcommand_passes_through", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
camp version
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough, got:\n%s", stdout)
		}
	})
}

func TestShellInit_ZshCgoWrapper(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/zsh-cgo-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "zsh")
	stub := stubCampScriptPosix(targetDir)

	stdout, exitCode := runZshScript(t, tc, initScript, stub, `
cgo foo
echo "PWD=$PWD"
`)
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "PWD="+targetDir) {
		t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
	}
}

func TestShellInit_ZshAliasWrappers(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/zsh-alias-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "zsh")
	stub := stubCampScriptPosix(targetDir)

	t.Run("cr_delegates_to_camp_run", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
cr echo hello
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cr, got:\n%s", stdout)
		}
	})

	t.Run("csw_delegates_to_camp_switch", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
csw mycamp
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s after csw, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("cint_delegates_to_camp_intent_add", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
cint "my idea"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cint, got:\n%s", stdout)
		}
	})

	t.Run("cie_delegates_to_camp_intent_explore", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
cie
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cie, got:\n%s", stdout)
		}
	})
}

func TestShellInit_ZshCompdefRegistered(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	// Verify the generated script contains the compdef directives
	initScript := shellInitScript(t, tc, "zsh")

	checks := []string{
		"compdef _cgo cgo",
		"compdef _camp camp",
		"compdef _csw csw",
	}
	for _, check := range checks {
		if !strings.Contains(initScript, check) {
			t.Errorf("zsh init missing: %s", check)
		}
	}
}

// ---------- Fish behavior tests ----------

func TestShellInit_FishCampWrapperGo(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/fish-nav-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "fish")
	stub := stubCampScriptFish(targetDir)

	t.Run("camp_go_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runFishScript(t, tc, initScript, stub, `
camp go foo
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_non_nav_subcommand_passes_through", func(t *testing.T) {
		stdout, exitCode := runFishScript(t, tc, initScript, stub, `
camp version
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough, got:\n%s", stdout)
		}
	})
}

func TestShellInit_FishCgoWrapper(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/fish-cgo-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "fish")
	stub := stubCampScriptFish(targetDir)

	stdout, exitCode := runFishScript(t, tc, initScript, stub, `
cgo foo
echo "PWD=$PWD"
`)
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "PWD="+targetDir) {
		t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
	}
}

func TestShellInit_FishAliasWrappers(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/fish-alias-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "fish")
	stub := stubCampScriptFish(targetDir)

	t.Run("cr_delegates_to_camp_run", func(t *testing.T) {
		stdout, exitCode := runFishScript(t, tc, initScript, stub, `
cr echo hello
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cr, got:\n%s", stdout)
		}
	})

	t.Run("cint_delegates_to_camp_intent_add", func(t *testing.T) {
		stdout, exitCode := runFishScript(t, tc, initScript, stub, `
cint "my idea"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cint, got:\n%s", stdout)
		}
	})
}
