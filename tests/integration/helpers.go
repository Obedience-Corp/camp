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
	"runtime"
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
	defer os.RemoveAll(filepath.Dir(campBinary))

	// Start container without bind-mounting the binary. Bind mounts go through
	// the host's overlayfs (Colima virtualisation layer on macOS) which can
	// serve stale or corrupted pages after heavy rm -rf / sync cycles, causing
	// non-deterministic SIGSEGV when the kernel page-faults the binary. Copying
	// the binary into the container's own writable layer avoids this entirely.
	// Pin to a specific digest rather than a floating tag. alpine:latest
	// resolves to a different layer on every cache miss, which means the git
	// version inside the container can silently change between CI runs. That
	// matters for worktree and submodule tests where git semantics differ
	// across minor versions. When bumping, update the digest in one place here
	// and verify the integration suite still passes.
	const alpineImage = "alpine:3.21@sha256:f27cad9117495d32d067133afff942cb2dc745dfe9163e949f6bfe8a6a245339"

	req := testcontainers.ContainerRequest{
		Image:      alpineImage,
		Cmd:        []string{"sleep", "3600"}, // Keep container running
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
		AutoRemove: true,
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	// Copy camp binary into the container's own filesystem layer (not a bind mount).
	if err := container.CopyFileToContainer(ctx, campBinary, "/camp", 0o755); err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to copy camp binary into container: %w", err)
	}

	// Build and copy fest binary (best-effort — fest is optional for most tests).
	festBinary, err := buildFestBinaryShared()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: fest binary not available: %v\n", err)
		festAvailable = false
	} else {
		defer os.RemoveAll(filepath.Dir(festBinary))
		if err := container.CopyFileToContainer(ctx, festBinary, "/usr/local/bin/fest", 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: failed to copy fest binary into container: %v\n", err)
			festAvailable = false
		} else {
			festAvailable = true
		}
	}

	// Build and copy scc binary (best-effort; scc is required by leverage tests
	// only). scc is a third-party Go binary at github.com/boyter/scc/v3 that the
	// `camp leverage` command shells out to via PATH lookup.
	sccBinary, err := buildSCCBinaryShared()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: scc binary not available: %v\n", err)
		sccAvailable = false
	} else {
		defer os.RemoveAll(filepath.Dir(sccBinary))
		if err := container.CopyFileToContainer(ctx, sccBinary, "/usr/local/bin/scc", 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: failed to copy scc binary into container: %v\n", err)
			sccAvailable = false
		} else {
			sccAvailable = true
		}
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

	// Verify camp binary was copied correctly
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

	binDir, err := os.MkdirTemp("", "camp-integration-bin-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary camp binary directory: %w", err)
	}

	binaryPath := filepath.Join(binDir, "camp")

	// Build the binary for Linux matching the host architecture.
	// Using runtime.GOARCH ensures native execution inside Colima's VM
	// (which matches the host arch). Hardcoding amd64 on an arm64 host
	// forces QEMU x86 emulation, causing non-deterministic SIGSEGV.
	//
	// Build with -tags=dev so dev-only commands (workitem, flow, quest)
	// are available for integration tests that exercise them. Stable-
	// profile gating is verified separately by unit tests.
	cmd := fmt.Sprintf("cd %s && GOOS=linux GOARCH=%s go build -tags=dev -o %s ./cmd/camp", projectRoot, runtime.GOARCH, binaryPath)
	if err := runCommand(cmd); err != nil {
		return "", fmt.Errorf("failed to build binary: %w", err)
	}

	return binaryPath, nil
}

// buildFestBinaryShared builds the fest binary from the sibling fest project.
// Returns ("", error) if the fest source is not found or build fails — callers
// should treat this as non-fatal since fest is optional for most integration tests.
func buildFestBinaryShared() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Navigate to camp project root (from tests/integration/)
	projectRoot := filepath.Join(cwd, "../..")
	projectRoot, err = filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// fest lives alongside camp as a sibling submodule under projects/
	festRoot := filepath.Join(projectRoot, "..", "fest")
	festRoot, err = filepath.Abs(festRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get fest absolute path: %w", err)
	}

	// Verify fest source exists
	if _, err := os.Stat(filepath.Join(festRoot, "cmd", "fest")); err != nil {
		return "", fmt.Errorf("fest source not found at %s: %w", festRoot, err)
	}

	binDir, err := os.MkdirTemp("", "fest-integration-bin-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary fest binary directory: %w", err)
	}

	binaryPath := filepath.Join(binDir, "fest")

	cmd := fmt.Sprintf("cd %s && GOOS=linux GOARCH=%s go build -o %s ./cmd/fest", festRoot, runtime.GOARCH, binaryPath)
	if err := runCommand(cmd); err != nil {
		return "", fmt.Errorf("failed to build fest binary: %w", err)
	}

	return binaryPath, nil
}

// buildSCCBinaryShared builds the third-party scc binary into a temp directory
// and returns its path. scc is required by the `camp leverage` command (it
// shells out to it for source-code counting). We build it on the host with
// GOOS=linux so the resulting binary runs in the alpine container, matching
// the camp/fest binary build pattern above.
//
// We deliberately use @latest rather than pinning a version. Real users
// install scc via `brew install scc` or `go install ...@latest`, so the
// integration tests should validate against whatever the leverage command
// will actually encounter in the wild. If a future scc release breaks the
// CLI contract leverage depends on, we'd rather find out here than in
// production.
//
// Returns ("", error) if the build fails. Callers should treat this as
// non-fatal since scc is only required by leverage tests.
func buildSCCBinaryShared() (string, error) {
	binDir, err := os.MkdirTemp("", "scc-integration-bin-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary scc binary directory: %w", err)
	}

	// Build scc from a throwaway Go module so we don't pollute the camp module.
	// `go install` with GOOS != host OS ignores GOBIN, so we use a tmp module
	// + `go build` pointing at the package import path.
	modDir := filepath.Join(binDir, "buildmod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create scc build module dir: %w", err)
	}

	binaryPath := filepath.Join(binDir, "scc")
	cmd := fmt.Sprintf(
		"cd %s && go mod init sccbuild >/dev/null 2>&1 && "+
			"go get github.com/boyter/scc/v3@latest && "+
			"GOOS=linux GOARCH=%s go build -o %s github.com/boyter/scc/v3",
		modDir, runtime.GOARCH, binaryPath,
	)
	if err := runCommand(cmd); err != nil {
		return "", fmt.Errorf("failed to build scc binary: %w", err)
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
