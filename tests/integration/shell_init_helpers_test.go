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

// stubCampScriptRecordArgsPosix creates a fake camp binary that records every
// invocation to /test/camp-args.log and prints a stable passthrough marker.
func stubCampScriptRecordArgsPosix() string {
	return `
rm -f /test/camp-args.log
_stub_dir=$(mktemp -d)
cat > "$_stub_dir/camp" << 'STUBEOF'
#!/bin/sh
printf '%s\n' "$*" >> /test/camp-args.log
echo "STUB_PASSTHROUGH"
STUBEOF
chmod +x "$_stub_dir/camp"
export PATH="$_stub_dir:$PATH"
`
}

// stubCampScriptRecordArgsFish is the fish equivalent of
// stubCampScriptRecordArgsPosix.
func stubCampScriptRecordArgsFish() string {
	return `
rm -f /test/camp-args.log
set -l _stub_dir (mktemp -d)
printf "#!/bin/sh\nprintf '%%s\n' \"\$*\" >> /test/camp-args.log\necho \"STUB_PASSTHROUGH\"\n" > $_stub_dir/camp
chmod +x $_stub_dir/camp
set -gx PATH $_stub_dir $PATH
`
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
