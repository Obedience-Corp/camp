//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// demuxDockerOutput strips Docker exec multiplexed stream headers from output.
// Docker exec output is multiplexed with 8-byte headers:
// - byte 0: stream type (1=stdout, 2=stderr)
// - bytes 1-3: padding (zeros)
// - bytes 4-7: big-endian uint32 payload size
func demuxDockerOutput(data []byte) []byte {
	var result bytes.Buffer
	offset := 0
	for offset < len(data) {
		// Need at least 8 bytes for header
		if offset+8 > len(data) {
			// Remaining bytes without header - append as-is
			result.Write(data[offset:])
			break
		}
		// Read payload size from bytes 4-7 (big-endian uint32)
		payloadSize := binary.BigEndian.Uint32(data[offset+4 : offset+8])
		// Skip header (8 bytes) and read payload
		payloadStart := offset + 8
		payloadEnd := payloadStart + int(payloadSize)
		if payloadEnd > len(data) {
			payloadEnd = len(data)
		}
		result.Write(data[payloadStart:payloadEnd])
		offset = payloadEnd
	}
	return result.Bytes()
}

// TestContainer wraps container operations for testing
type TestContainer struct {
	container testcontainers.Container
	ctx       context.Context
	t         *testing.T
}

// NewSharedContainer creates a container for reuse across multiple tests.
// Unlike NewTestContainer, this doesn't take a testing.T since it's called
// from TestMain before any individual tests run.
func NewSharedContainer() (*TestContainer, error) {
	ctx := context.Background()

	// Build camp binary first
	campBinary, err := buildCampBinaryShared()
	if err != nil {
		return nil, fmt.Errorf("failed to build camp binary: %w", err)
	}

	req := testcontainers.ContainerRequest{
		Image:      "alpine:latest",
		Cmd:        []string{"sleep", "3600"}, // Keep container running
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
		AutoRemove: true,
		Mounts: testcontainers.ContainerMounts{
			{
				Source:   testcontainers.GenericBindMountSource{HostPath: campBinary},
				Target:   "/camp",
				ReadOnly: false,
			},
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	// Install git (required for project operations)
	exitCode, output, err := container.Exec(ctx, []string{"apk", "add", "--no-cache", "git"})
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to install git: %w", err)
	}
	if exitCode != 0 {
		outputBytes, _ := io.ReadAll(output)
		container.Terminate(ctx)
		return nil, fmt.Errorf("apk add git failed with exit code %d: %s", exitCode, string(outputBytes))
	}

	// Configure git (required for submodule operations)
	exitCode, _, err = container.Exec(ctx, []string{"git", "config", "--global", "user.email", "test@test.com"})
	if err != nil || exitCode != 0 {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to configure git email: %w", err)
	}
	exitCode, _, err = container.Exec(ctx, []string{"git", "config", "--global", "user.name", "Test User"})
	if err != nil || exitCode != 0 {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to configure git name: %w", err)
	}

	// Check if camp binary exists and make it executable
	exitCode, output, err = container.Exec(ctx, []string{"ls", "-la", "/camp"})
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to check camp binary: %w", err)
	}
	if exitCode != 0 {
		outputBytes, _ := io.ReadAll(output)
		container.Terminate(ctx)
		return nil, fmt.Errorf("camp binary not found, ls output: %s", string(outputBytes))
	}

	// Make camp executable in container
	exitCode, output, err = container.Exec(ctx, []string{"chmod", "+x", "/camp"})
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to make camp executable: %w", err)
	}
	if exitCode != 0 {
		outputBytes, _ := io.ReadAll(output)
		container.Terminate(ctx)
		return nil, fmt.Errorf("chmod failed with exit code %d, output: %s", exitCode, string(outputBytes))
	}

	// Create initial working directories
	exitCode, _, err = container.Exec(ctx, []string{"mkdir", "-p", "/test", "/campaigns", "/root/.config/camp"})
	if err != nil || exitCode != 0 {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to create initial directories: %w", err)
	}

	return &TestContainer{
		container: container,
		ctx:       ctx,
		t:         nil, // No test context yet - will be set per-test
	}, nil
}

// buildCampBinaryShared builds the camp binary without test context logging
func buildCampBinaryShared() (string, error) {
	// Get the project root directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Navigate to project root (from tests/integration/)
	projectRoot := filepath.Join(cwd, "../..")
	projectRoot, err = filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Build to bin/linux directory in project root (accessible to Docker)
	binDir := filepath.Join(projectRoot, "bin", "linux")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin/linux directory: %w", err)
	}

	binaryPath := filepath.Join(binDir, "camp")

	// Build the binary for Linux (required for Alpine containers)
	cmd := fmt.Sprintf("cd %s && GOOS=linux GOARCH=amd64 go build -o %s ./cmd/camp", projectRoot, binaryPath)
	if err := runCommand(cmd); err != nil {
		return "", fmt.Errorf("failed to build binary: %w", err)
	}

	return binaryPath, nil
}

// runCommand executes a shell command
func runCommand(cmd string) error {
	if cmd == "" {
		return fmt.Errorf("empty command")
	}

	c := exec.Command("sh", "-c", cmd)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// Reset clears container state between tests.
// This removes all test artifacts while keeping the container and binary intact.
func (tc *TestContainer) Reset() error {
	// Remove all test artifacts and recreate clean directories
	exitCode, _, err := tc.container.Exec(tc.ctx, []string{
		"sh", "-c",
		"rm -rf /test /campaigns /root/.config/camp /root/.camp 2>/dev/null; " +
			"mkdir -p /test /campaigns /root/.config/camp",
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
		tc.container.Terminate(tc.ctx)
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
	// Use sh -c to change directory first
	cmdStr := fmt.Sprintf("cd %s && /camp %s", dir, strings.Join(quotedArgs, " "))
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
