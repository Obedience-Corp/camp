//go:build integration
// +build integration

package integration

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Reset clears container state between tests.
// This removes all test artifacts while keeping the container and binary intact.
// The trailing `sync` ensures filesystem buffers are flushed before the next test
// begins — required for consistency on macOS/Colima where Docker exec runs through
// a virtualization layer (overlayfs in a Linux VM).
func (tc *TestContainer) Reset() error {
	// Remove all test artifacts and recreate clean directories
	exitCode, _, err := tc.container.Exec(tc.ctx, []string{
		"sh", "-c",
		"rm -rf /test /campaigns /root/.config/camp /root/.camp 2>/dev/null; " +
			"mkdir -p /test /campaigns /root/.config/camp; sync",
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
		_ = tc.container.Terminate(tc.ctx)
	}
}

// RunCamp runs the camp command in the container
func (tc *TestContainer) RunCamp(args ...string) (string, error) {
	cmd := append([]string{"/camp"}, args...)

	exitCode, reader, err := tc.container.Exec(tc.ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("failed to execute camp: %w", err)
	}

	rawOutput, err := io.ReadAll(reader)
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
	cmdStr := fmt.Sprintf("cd %s && /camp %s 2>&1", dir, strings.Join(quotedArgs, " "))
	cmd := []string{"sh", "-c", cmdStr}

	exitCode, reader, err := tc.container.Exec(tc.ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("failed to execute camp: %w", err)
	}

	rawOutput, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read output: %w", err)
	}

	output := demuxDockerOutput(rawOutput)

	if exitCode != 0 {
		return string(output), fmt.Errorf("camp exited with code %d: %s", exitCode, output)
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
	exitCode, reader, gitErr := tc.container.Exec(tc.ctx, []string{"sh", "-c", cmdStr})
	if gitErr != nil {
		return output, fmt.Errorf("failed to init git repo: %w", gitErr)
	}
	if exitCode != 0 {
		rawOutput, _ := io.ReadAll(reader)
		return output, fmt.Errorf("git init failed: %s", string(demuxDockerOutput(rawOutput)))
	}

	return output, nil
}

// ReadFile reads a file from the container
func (tc *TestContainer) ReadFile(path string) (string, error) {
	exitCode, reader, err := tc.container.Exec(tc.ctx, []string{"cat", path})
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	rawOutput, err := io.ReadAll(reader)
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
	exitCode, _, err := tc.container.Exec(tc.ctx, []string{"mkdir", "-p", dir})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Write content using printf to handle special characters
	exitCode, _, err = tc.container.Exec(tc.ctx, []string{
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
	exitCode, _, err := tc.container.Exec(tc.ctx, []string{"test", "-f", path})
	if err != nil {
		return false, fmt.Errorf("failed to check file: %w", err)
	}
	return exitCode == 0, nil
}

// CheckDirExists checks if a directory exists in the container
func (tc *TestContainer) CheckDirExists(path string) (bool, error) {
	exitCode, _, err := tc.container.Exec(tc.ctx, []string{"test", "-d", path})
	if err != nil {
		return false, fmt.Errorf("failed to check directory: %w", err)
	}
	return exitCode == 0, nil
}

// ListDirectory lists files in a container directory
func (tc *TestContainer) ListDirectory(path string) ([]string, error) {
	exitCode, reader, err := tc.container.Exec(tc.ctx, []string{"find", path, "-type", "f"})
	if err != nil {
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}

	if exitCode != 0 {
		return nil, fmt.Errorf("find command failed with exit code %d", exitCode)
	}

	rawOutput, err := io.ReadAll(reader)
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
	exitCode, _, err := tc.container.Exec(tc.ctx, []string{"mkdir", "-p", path})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Initialize git repo
	exitCode, output, err := tc.container.Exec(tc.ctx, []string{"git", "init", path})
	if err != nil {
		return fmt.Errorf("failed to init git repo: %w", err)
	}
	if exitCode != 0 {
		outputBytes, _ := io.ReadAll(output)
		return fmt.Errorf("git init failed: %s", string(outputBytes))
	}

	// Create an initial commit so the repo is valid
	cmdStr := fmt.Sprintf("cd %s && touch .gitkeep && git add . && git commit -m 'Initial commit'", path)
	exitCode, output, err = tc.container.Exec(tc.ctx, []string{"sh", "-c", cmdStr})
	if err != nil {
		return fmt.Errorf("failed to create initial commit: %w", err)
	}
	if exitCode != 0 {
		outputBytes, _ := io.ReadAll(output)
		return fmt.Errorf("initial commit failed: %s", string(outputBytes))
	}

	return nil
}

// ExecCommand executes an arbitrary command in the container
func (tc *TestContainer) ExecCommand(args ...string) (string, int, error) {
	exitCode, reader, err := tc.container.Exec(tc.ctx, args)
	if err != nil {
		return "", -1, fmt.Errorf("failed to execute command: %w", err)
	}

	rawOutput, err := io.ReadAll(reader)
	if err != nil {
		return "", exitCode, fmt.Errorf("failed to read output: %w", err)
	}

	output := demuxDockerOutput(rawOutput)
	return string(output), exitCode, nil
}

// RunCampSplit runs a camp command inside the container and returns stdout and
// stderr as separate strings along with the exit code. Stdout and stderr are
// captured to temporary files inside the container and read back individually.
// This is used by tests that need to distinguish machine output (stdout) from
// human-readable output (stderr) when --print-path is in effect.
func (tc *TestContainer) RunCampSplit(args ...string) (stdout, stderr string, exitCode int, err error) {
	// Quote args for safe shell embedding.
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		quotedArgs[i] = "'" + escaped + "'"
	}
	cmdStr := fmt.Sprintf(
		"/camp %s >/tmp/_camp_stdout 2>/tmp/_camp_stderr; echo $? >/tmp/_camp_exitcode",
		strings.Join(quotedArgs, " "),
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
