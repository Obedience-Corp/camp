//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

// execTimeout bounds a single container exec. The longest legitimate command
// in the suite is a `sleep 3` fixture plus ordinary git/camp invocations, so
// two minutes is >10x margin. Without a bound, an exec against a wedged
// daemon parks forever: the 2026-08-05 goroutine dump showed execs sleeping
// 14+ minutes while every other test starved behind them, until go test's own
// timeout panicked with no failing assertion anywhere in the output.
const execTimeout = 2 * time.Minute

// exec runs a command in the container, bounded by execTimeout, classifying
// transport faults and reporting them to the run-level infrastructure latch.
//
// Every container command in this file goes through here rather than calling
// container.Exec directly, so a Docker transport failure is labelled once,
// where it happens.
//
// The failure it exists for looks like this:
//
//	--- FAIL: TestFresh_ListRejectsPositionalArg
//	    failed to init git repo: container exec attach: unexpected EOF
//
// That names a test about positional arguments, so the reader goes and studies
// argument handling. Nothing is wrong with the code under test: the Docker
// daemon dropped the exec stream, usually because several suites are running
// at once. A harness that reports its own infrastructure as a failure of
// whatever test happened to be running costs more time than the flake does.
//
// Labelling alone proved insufficient: the 2026-08-10 collapse rendered six
// infra-labelled failures as six broken tests because nothing downstream
// could tell them apart, and the run kept dispatching tests into a dead
// daemon. Infra faults now also record their pool member in runInfra (the
// same distinct-member latch Reset checkouts use), so a dying daemon stops
// the run early with one banner instead of a trickle of plausible failures.
func (tc *TestContainer) exec(ctx context.Context, cmd []string) (int, io.Reader, error) {
	return tc.execWith(ctx, cmd, true)
}

// execUnrecorded is exec without the latch report. Reset uses it: Reset has
// its own retry-then-record protocol where a single transient fault is
// absorbed by the retry, and recording that first attempt here would strip
// the forgiveness the retry exists to provide.
func (tc *TestContainer) execUnrecorded(ctx context.Context, cmd []string) (int, io.Reader, error) {
	return tc.execWith(ctx, cmd, false)
}

func (tc *TestContainer) execWith(ctx context.Context, cmd []string, record bool) (int, io.Reader, error) {
	execCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	start := time.Now()
	exitCode, reader, err := tc.container.Exec(execCtx, cmd)
	var buf []byte
	if err == nil && reader != nil {
		// Drain inside the deadline. The reader is a live hijacked connection
		// whose lifetime is bound to execCtx: handing it to the caller after
		// cancel() would truncate output, and an unread stream from a wedged
		// daemon would block the caller with no bound at all. Exec has already
		// waited for the command to exit, so this is a buffered read, not a
		// wait.
		buf, err = io.ReadAll(reader)
	}
	observeExecDuration(time.Since(start))

	if err != nil {
		err = classifyExecOutcome(err, execCtx.Err() != nil && ctx.Err() == nil)
		var ie *infraError
		if record && errors.As(err, &ie) {
			runInfra.recordMember(tc.container.GetContainerID(), ie.Error())
		}
	}
	return exitCode, bytes.NewReader(buf), err
}

// infraError marks a fault of the Docker transport rather than of the code
// under test. It is a type, not a string prefix, so exec can route these into
// the run-level latch without re-parsing its own messages.
type infraError struct {
	msg   string
	cause error
}

func (e *infraError) Error() string { return e.msg }
func (e *infraError) Unwrap() error { return e.cause }

func newInfraError(cause error, format string, args ...any) *infraError {
	return &infraError{
		msg:   "INFRASTRUCTURE FAILURE (not a test failure): " + fmt.Sprintf(format, args...),
		cause: cause,
	}
}

// execTransportSignatures are the Docker-side failures that mean the daemon or
// the exec stream died, rather than the command reporting something.
var execTransportSignatures = []string{
	"exec attach",
	"unexpected EOF",
	"connection reset by peer",
	"broken pipe",
	"is not running",
	"No such container",
	"Cannot connect to the Docker daemon",
}

// classifyExecOutcome maps the raw error from a bounded exec to its final
// form. execTimedOut is true when the per-exec deadline fired while the
// test's own context was still live — that is the wedged-daemon case, which
// carries no transport signature of its own.
func classifyExecOutcome(err error, execTimedOut bool) error {
	if err == nil {
		return nil
	}
	if execTimedOut {
		return newInfraError(err,
			"container exec did not complete within %s: %v"+
				"\n\nThe Docker daemon stopped servicing this exec (wedged or "+
				"overloaded). Common cause: several suites or gates running at "+
				"once. Re-run on an idle machine, or lower CAMP_TEST_POOL_SIZE",
			execTimeout, err)
	}
	return classifyExecError(err)
}

// classifyExecError labels a transport fault so it cannot be read as a test
// failure. Anything else is returned unchanged.
func classifyExecError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, sig := range execTransportSignatures {
		if strings.Contains(msg, sig) {
			return newInfraError(err,
				"container exec did not complete: %v"+
					"\n\nThe Docker daemon dropped the exec stream. Common cause: several "+
					"suites or gates running at once. Re-run on an idle machine, or lower "+
					"CAMP_TEST_POOL_SIZE", err)
		}
	}
	return err
}

// readExecOutput consumes an exec stream, classifying transport loss that
// surfaces mid-read. Exec hands back a live hijacked connection, so the daemon
// dropping it after Exec returned nil reports here, from the read, as an
// ordinary "unexpected EOF". Without classification on this second channel the
// consumers wrap it as "failed to read output" and recreate exactly the
// misleading test failure this file exists to remove.
func readExecOutput(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return data, classifyExecError(err)
	}
	return data, nil
}

// Reset clears container state between tests.
// This removes all test artifacts while keeping the container and binary intact.
// The trailing `sync` ensures filesystem buffers are flushed before the next test
// begins — required for consistency on macOS/Colima where Docker exec runs through
// a virtualization layer (overlayfs in a Linux VM).
//
// The default global config seeds dungeon_hidden=false so the bulk of this
// suite (written against the legacy visible "dungeon" spelling) keeps
// exercising real dungeon mechanics without every test needing to know about
// the setting. Tests that specifically cover the hidden-by-default behavior
// or the dual-spelling resolution override this fixture explicitly via
// WriteGlobalConfig.
func (tc *TestContainer) Reset() error {
	// A deferral opt-in belongs to one test only.
	tc.deferral = false

	// Remove all test artifacts and recreate clean directories. Include both
	// current and legacy config homes so registry/global settings never leak
	// between tests that share a pooled container.
	//
	// execUnrecorded, not exec: GetSharedContainer retries a failed Reset and
	// records the member itself only on double failure. Letting exec report
	// the first attempt would arm members for blips the retry absorbs.
	exitCode, _, err := tc.execUnrecorded(tc.ctx, []string{
		"sh", "-c",
		// Kill detached deferred-commit workers first. Camp spawns them with
		// setsid so they outlive the command that started them, which is the
		// point in production and a leak in a pooled container: a worker from
		// the previous test would still be scanning directories the next test
		// is creating. Clearing files alone does not stop a running process.
		// The bracket is not cosmetic: pkill -f matches full command lines,
		// and this very shell's command line contains the pattern. Written
		// plainly it kills itself and the rest of the reset never runs, which
		// presents as every later test failing in its setup.
		"pkill -f '[j]obs run' 2>/dev/null; " +
			"rm -rf /test /campaigns /root/.obey /root/.config/camp /root/.camp /tmp/create-* 2>/dev/null; " +
			"mkdir -p /test /campaigns /root/.config/camp /root/.obey/campaign; sync; " +
			`printf '{"dungeon_hidden": false}' > /root/.obey/campaign/config.json`,
	})
	if err != nil {
		return fmt.Errorf("failed to reset container: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("reset command failed with exit code %d", exitCode)
	}
	return nil
}

// Cleanup terminates the container
func (tc *TestContainer) Cleanup() {
	if tc.container != nil {
		// A pooled container holds nothing worth a graceful shutdown; the
		// default 10s stop grace was pure teardown latency.
		_ = tc.container.Terminate(tc.ctx, testcontainers.StopTimeout(time.Second))
	}
}

// RunCamp runs the camp command in the container
func (tc *TestContainer) RunCamp(args ...string) (string, error) {
	cmd := append([]string{"/camp"}, args...)

	exitCode, reader, err := tc.exec(tc.ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("failed to execute camp: %w", err)
	}

	rawOutput, err := readExecOutput(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read output: %w", err)
	}

	// Strip Docker exec multiplexed stream headers
	output := demuxDockerOutput(rawOutput)

	if exitCode != 0 {
		return string(output), fmt.Errorf("camp exited with code %d: %s", exitCode, output)
	}

	return string(output), nil
}

// RunCampInDir runs the camp command from a specific directory
func (tc *TestContainer) RunCampInDir(dir string, args ...string) (string, error) {
	// Quote each argument to handle spaces properly
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		// Escape single quotes in the arg and wrap in single quotes
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		quotedArgs[i] = "'" + escaped + "'"
	}
	// Use sh -c to change directory first, redirect stderr to stdout for error capture
	cmdStr := fmt.Sprintf("cd %s && %s/camp %s 2>&1",
		dir, tc.campEnvPrefix(), strings.Join(quotedArgs, " "))
	cmd := []string{"sh", "-c", cmdStr}

	exitCode, reader, err := tc.exec(tc.ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("failed to execute camp: %w", err)
	}

	rawOutput, err := readExecOutput(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read output: %w", err)
	}

	output := demuxDockerOutput(rawOutput)

	if exitCode != 0 {
		return string(output), fmt.Errorf("camp exited with code %d: %s", exitCode, output)
	}

	return string(output), nil
}

// RunLegacyCampInDir runs the pinned pre-reader camp binary (/camp-legacy) from
// dir. Used only by the criterion-17 rollout-contract test to prove a pre-reader
// binary hard-fails on a v1alpha8 marker. Mirrors RunCampInDir: the returned
// error is non-nil on a non-zero exit, with the merged stdout/stderr as output.
func (tc *TestContainer) RunLegacyCampInDir(dir string, args ...string) (string, error) {
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		quotedArgs[i] = "'" + escaped + "'"
	}
	cmdStr := fmt.Sprintf("cd %s && /camp-legacy %s 2>&1", dir, strings.Join(quotedArgs, " "))
	exitCode, reader, err := tc.exec(tc.ctx, []string{"sh", "-c", cmdStr})
	if err != nil {
		return "", fmt.Errorf("failed to execute legacy camp: %w", err)
	}
	rawOutput, err := readExecOutput(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read output: %w", err)
	}
	output := demuxDockerOutput(rawOutput)
	if exitCode != 0 {
		return string(output), fmt.Errorf("legacy camp exited with code %d: %s", exitCode, output)
	}
	return string(output), nil
}

// InitCampaign creates a new campaign via camp init and initializes it as a git repo
func (tc *TestContainer) InitCampaign(path, name, campType string) (string, error) {
	args := []string{"init", path, "--name", name, "-d", "Test campaign", "-m", "Test mission"}
	if campType != "" {
		args = append(args, "--type", campType)
	}
	output, err := tc.RunCamp(args...)
	if err != nil {
		return output, err
	}

	// Initialize campaign as git repo (required for submodule operations)
	cmdStr := fmt.Sprintf("cd %s && git init && git add . && git commit -m 'Initial campaign setup'", path)
	exitCode, reader, gitErr := tc.exec(tc.ctx, []string{"sh", "-c", cmdStr})
	if gitErr != nil {
		return output, fmt.Errorf("failed to init git repo: %w", gitErr)
	}
	if exitCode != 0 {
		rawOutput, _ := readExecOutput(reader)
		return output, fmt.Errorf("git init failed: %s", string(demuxDockerOutput(rawOutput)))
	}

	return output, nil
}

// ReadFile reads a file from the container
func (tc *TestContainer) ReadFile(path string) (string, error) {
	exitCode, reader, err := tc.exec(tc.ctx, []string{"cat", path})
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	rawOutput, err := readExecOutput(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read output: %w", err)
	}

	output := demuxDockerOutput(rawOutput)

	if exitCode != 0 {
		return "", fmt.Errorf("cat command failed with exit code %d: %s", exitCode, output)
	}

	return string(output), nil
}

// WriteFile writes content to a file in the container
func (tc *TestContainer) WriteFile(path, content string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	exitCode, _, err := tc.exec(tc.ctx, []string{"mkdir", "-p", dir})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Write content using printf to handle special characters
	exitCode, _, err = tc.exec(tc.ctx, []string{
		"sh", "-c",
		fmt.Sprintf("printf '%%s' '%s' > %s", strings.ReplaceAll(content, "'", "'\\''"), path),
	})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// CheckFileExists checks if a file exists in the container
func (tc *TestContainer) CheckFileExists(path string) (bool, error) {
	exitCode, _, err := tc.exec(tc.ctx, []string{"test", "-f", path})
	if err != nil {
		return false, fmt.Errorf("failed to check file: %w", err)
	}
	return exitCode == 0, nil
}

// CheckDirExists checks if a directory exists in the container
func (tc *TestContainer) CheckDirExists(path string) (bool, error) {
	exitCode, _, err := tc.exec(tc.ctx, []string{"test", "-d", path})
	if err != nil {
		return false, fmt.Errorf("failed to check directory: %w", err)
	}
	return exitCode == 0, nil
}

// ListDirectory lists files in a container directory
func (tc *TestContainer) ListDirectory(path string) ([]string, error) {
	exitCode, reader, err := tc.exec(tc.ctx, []string{"find", path, "-type", "f"})
	if err != nil {
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}

	if exitCode != 0 {
		return nil, fmt.Errorf("find command failed with exit code %d", exitCode)
	}

	rawOutput, err := readExecOutput(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read output: %w", err)
	}

	output := demuxDockerOutput(rawOutput)

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var files []string
	for _, line := range lines {
		if line != "" && line != path {
			files = append(files, line)
		}
	}

	return files, nil
}

// CreateGitRepo initializes a git repository at the given path
func (tc *TestContainer) CreateGitRepo(path string) error {
	// Create directory
	exitCode, _, err := tc.exec(tc.ctx, []string{"mkdir", "-p", path})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Initialize git repo
	exitCode, output, err := tc.exec(tc.ctx, []string{"git", "init", path})
	if err != nil {
		return fmt.Errorf("failed to init git repo: %w", err)
	}
	if exitCode != 0 {
		outputBytes, _ := readExecOutput(output)
		return fmt.Errorf("git init failed: %s", string(outputBytes))
	}

	// Create an initial commit so the repo is valid
	cmdStr := fmt.Sprintf("cd %s && touch .gitkeep && git add . && git commit -m 'Initial commit'", path)
	exitCode, output, err = tc.exec(tc.ctx, []string{"sh", "-c", cmdStr})
	if err != nil {
		return fmt.Errorf("failed to create initial commit: %w", err)
	}
	if exitCode != 0 {
		outputBytes, _ := readExecOutput(output)
		return fmt.Errorf("initial commit failed: %s", string(outputBytes))
	}

	return nil
}

// ExecCommand executes an arbitrary command in the container
func (tc *TestContainer) ExecCommand(args ...string) (string, int, error) {
	exitCode, reader, err := tc.exec(tc.ctx, args)
	if err != nil {
		return "", -1, fmt.Errorf("failed to execute command: %w", err)
	}

	rawOutput, err := readExecOutput(reader)
	if err != nil {
		return "", exitCode, fmt.Errorf("failed to read output: %w", err)
	}

	output := demuxDockerOutput(rawOutput)
	return string(output), exitCode, nil
}

// RunCampSplit runs a camp command inside the container and returns stdout and
// stderr as separate strings along with the exit code. Stdout and stderr are
// captured to temporary files inside the container and read back individually.
func (tc *TestContainer) RunCampSplit(args ...string) (stdout, stderr string, exitCode int, err error) {
	// Quote args for safe shell embedding.
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		quotedArgs[i] = "'" + escaped + "'"
	}
	cmdStr := fmt.Sprintf(
		"%s/camp %s >/tmp/_camp_stdout 2>/tmp/_camp_stderr; echo $? >/tmp/_camp_exitcode",
		tc.campEnvPrefix(), strings.Join(quotedArgs, " "),
	)
	if _, _, err = tc.ExecCommand("sh", "-c", cmdStr); err != nil {
		return "", "", -1, fmt.Errorf("RunCampSplit exec failed: %w", err)
	}
	stdoutRaw, _, _ := tc.ExecCommand("cat", "/tmp/_camp_stdout")
	stderrRaw, _, _ := tc.ExecCommand("cat", "/tmp/_camp_stderr")
	exitStr, _, _ := tc.ExecCommand("cat", "/tmp/_camp_exitcode")
	exitCode = 0
	if s := strings.TrimSpace(exitStr); s != "" && s != "0" {
		exitCode = 1
	}
	// Clean up temp files (best-effort).
	_, _, _ = tc.ExecCommand("rm", "-f", "/tmp/_camp_stdout", "/tmp/_camp_stderr", "/tmp/_camp_exitcode")
	return stdoutRaw, stderrRaw, exitCode, nil
}

// RunCampSplitInDir runs camp from dir and returns stdout and stderr as
// separate strings along with the exit code, mirroring RunCampSplit but
// scoped to a working directory the way RunCampInDir is for combined output.
func (tc *TestContainer) RunCampSplitInDir(dir string, args ...string) (stdout, stderr string, exitCode int, err error) {
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		quotedArgs[i] = "'" + escaped + "'"
	}
	cmdStr := fmt.Sprintf(
		"cd %s && %s/camp %s >/tmp/_camp_stdout 2>/tmp/_camp_stderr; echo $? >/tmp/_camp_exitcode",
		dir, tc.campEnvPrefix(), strings.Join(quotedArgs, " "),
	)
	if _, _, err = tc.ExecCommand("sh", "-c", cmdStr); err != nil {
		return "", "", -1, fmt.Errorf("RunCampSplitInDir exec failed: %w", err)
	}
	stdoutRaw, _, _ := tc.ExecCommand("cat", "/tmp/_camp_stdout")
	stderrRaw, _, _ := tc.ExecCommand("cat", "/tmp/_camp_stderr")
	exitStr, _, _ := tc.ExecCommand("cat", "/tmp/_camp_exitcode")
	exitCode = 0
	if s := strings.TrimSpace(exitStr); s != "" && s != "0" {
		exitCode = 1
	}
	_, _, _ = tc.ExecCommand("rm", "-f", "/tmp/_camp_stdout", "/tmp/_camp_stderr", "/tmp/_camp_exitcode")
	return stdoutRaw, stderrRaw, exitCode, nil
}

// WriteGlobalConfig writes a JSON snippet to the global config path inside the
// container. This lets tests set campaigns_dir without running 'camp settings'.
func (tc *TestContainer) WriteGlobalConfig(content string) error {
	// Ensure the config directory exists.
	if err := tc.WriteFile("/root/.obey/campaign/config.json", content); err != nil {
		return fmt.Errorf("WriteGlobalConfig: %w", err)
	}
	return nil
}

// Shell runs a shell script inside the container via `sh -lc` and fails the
// test if the command errors or exits non-zero. Returns combined stdout+stderr.
// Intended for setup-heavy test fixtures where the natural authoring form is a
// multi-line shell block.
func (tc *TestContainer) Shell(t *testing.T, script string) string {
	t.Helper()

	output, exitCode, err := tc.ExecCommand("sh", "-lc", script)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "shell command failed:\n%s", output)
	return output
}

// GitOutput runs `git -C <dir> <args...>` inside the container and returns
// trimmed stdout+stderr. Fails the test if the command errors or exits
// non-zero. Use this for assertions about git state (branch names, worktree
// listings, rev-parse output) where the caller wants the exact string.
func (tc *TestContainer) GitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := append([]string{"git", "-C", dir}, args...)
	output, exitCode, err := tc.ExecCommand(cmd...)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "git %v failed:\n%s", args, output)
	return strings.TrimSpace(output)
}
