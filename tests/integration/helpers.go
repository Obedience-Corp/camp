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

// sharedBinaries holds host paths to the test binaries built once in TestMain
// and copied into every pooled container.
type sharedBinaries struct {
	camp       string
	fest       string // "" when fest is unavailable
	scc        string // "" when scc is unavailable
	campLegacy string // "" when the pinned pre-reader binary is unavailable
}

// buildSharedBinaries builds the camp/fest/scc binaries on the host exactly once.
// The returned cleanup removes their temp directories; call it only after every
// pooled container has copied the binaries in. fest/scc are best-effort and set
// festAvailable/sccAvailable; camp is required.
func buildSharedBinaries() (sharedBinaries, func(), error) {
	var dirs []string
	cleanup := func() {
		for _, d := range dirs {
			_ = os.RemoveAll(d)
		}
	}

	campBinary, err := buildCampBinaryShared()
	if err != nil {
		cleanup()
		return sharedBinaries{}, func() {}, fmt.Errorf("failed to build camp binary: %w", err)
	}
	dirs = append(dirs, filepath.Dir(campBinary))
	bins := sharedBinaries{camp: campBinary}

	// fest is optional for most tests.
	if festBinary, err := buildFestBinaryShared(); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: fest binary not available: %v\n", err)
		festAvailable = false
	} else {
		dirs = append(dirs, filepath.Dir(festBinary))
		bins.fest = festBinary
		festAvailable = true
	}

	// scc is required only by leverage tests. Third-party binary at
	// github.com/boyter/scc/v3 that `camp leverage` shells out to via PATH.
	if sccBinary, err := buildSCCBinaryShared(); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: scc binary not available: %v\n", err)
		sccAvailable = false
	} else {
		dirs = append(dirs, filepath.Dir(sccBinary))
		bins.scc = sccBinary
		sccAvailable = true
	}

	// Pinned pre-reader binary for the criterion-17 rollout-contract test. Cached
	// across runs in TempDir (so not added to dirs for cleanup); a skip reason is
	// recorded instead of a hard failure when the pinned commit is unavailable.
	if legacyBin, skip := buildLegacyCampBinaryShared(); skip != "" {
		fmt.Fprintf(os.Stderr, "WARN: %s\n", skip)
		legacyCampSkip = skip
	} else {
		bins.campLegacy = legacyBin
	}

	return bins, cleanup, nil
}

// legacyCampCommit pins the pre-reader camp binary for the criterion-17 rollout
// contract test: commit 1f06e423 is release v0.3.0-rc.2, the newest shipped
// binary whose allowlist stops at v1alpha6 with no forward-compat rule.
const legacyCampCommit = "1f06e423"

// buildLegacyCampBinaryShared builds the pinned pre-reader camp binary once and
// caches it across runs at a stable TempDir path keyed by commit and arch. It
// returns a non-empty skip reason (and empty path) when the pinned commit is not
// present in this clone (e.g. a shallow CI checkout) or the build fails, so the
// criterion-17 test skips with a clear message rather than a cryptic failure that
// blocks the whole suite. It stays entirely inside the integration harness: no
// host unit test depends on git history.
func buildLegacyCampBinaryShared() (path string, skip string) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Sprintf("pre-reader binary unavailable: %v", err)
	}
	projectRoot, err := filepath.Abs(filepath.Join(cwd, "../.."))
	if err != nil {
		return "", fmt.Sprintf("pre-reader binary unavailable: %v", err)
	}

	cached := filepath.Join(os.TempDir(), fmt.Sprintf("camp-legacy-%s-%s", legacyCampCommit, runtime.GOARCH))
	if _, statErr := os.Stat(cached); statErr == nil {
		return cached, ""
	}

	// cat-file -e fails on a shallow clone that lacks the pinned commit.
	if err := runCommand(fmt.Sprintf("git -C %s cat-file -e %s^{commit}", projectRoot, legacyCampCommit)); err != nil {
		return "", fmt.Sprintf("pre-reader binary skipped: commit %s (v0.3.0-rc.2) not in this clone (shallow?)", legacyCampCommit)
	}

	srcDir, err := os.MkdirTemp("", "camp-legacy-src-*")
	if err != nil {
		return "", fmt.Sprintf("pre-reader binary skipped: %v", err)
	}
	defer os.RemoveAll(srcDir)
	// git archive extracts the source tree at the pinned commit with no worktree
	// or .git entry to clean up afterward.
	if err := runCommand(fmt.Sprintf("git -C %s archive %s | tar -x -C %s", projectRoot, legacyCampCommit, srcDir)); err != nil {
		return "", fmt.Sprintf("pre-reader binary skipped: extract %s failed: %v", legacyCampCommit, err)
	}

	// Build to a pid-suffixed temp path then rename into the cache, so an
	// interrupted or racing build never leaves a partial binary at the cached
	// path. GOTOOLCHAIN=auto lets go fetch whatever toolchain the rc.2 go.mod pins.
	tmpBin := fmt.Sprintf("%s.tmp.%d", cached, os.Getpid())
	build := fmt.Sprintf("cd %s && GOTOOLCHAIN=auto GOOS=linux GOARCH=%s go build -tags=dev -o %s ./cmd/camp",
		srcDir, runtime.GOARCH, tmpBin)
	if err := runCommand(build); err != nil {
		_ = os.Remove(tmpBin)
		return "", fmt.Sprintf("pre-reader binary skipped: build %s failed: %v", legacyCampCommit, err)
	}
	if err := os.Rename(tmpBin, cached); err != nil {
		_ = os.Remove(tmpBin)
		return "", fmt.Sprintf("pre-reader binary skipped: cache %s failed: %v", legacyCampCommit, err)
	}
	return cached, ""
}

// newPooledContainer starts one container and provisions it identically to every
// other pool member: copy the prebuilt binaries in, install/configure git, and
// create the working directories. Tests check these out from the pool, run with
// t.Parallel(), and return them via Reset(); each test therefore has exclusive
// use of an isolated container filesystem, so the hardcoded /test and /campaigns
// paths never collide across parallel tests.
func newPooledContainer(ctx context.Context, bins sharedBinaries) (*TestContainer, error) {
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
	if err := container.CopyFileToContainer(ctx, bins.camp, "/camp", 0o755); err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to copy camp binary into container: %w", err)
	}

	// Copy fest/scc when available. Build succeeded for the whole pool, so a copy
	// failure here is a real container fault: fail this member rather than leave
	// the pool with mixed availability.
	if bins.fest != "" {
		if err := container.CopyFileToContainer(ctx, bins.fest, "/usr/local/bin/fest", 0o755); err != nil {
			container.Terminate(ctx)
			return nil, fmt.Errorf("failed to copy fest binary into container: %w", err)
		}
	}
	if bins.scc != "" {
		if err := container.CopyFileToContainer(ctx, bins.scc, "/usr/local/bin/scc", 0o755); err != nil {
			container.Terminate(ctx)
			return nil, fmt.Errorf("failed to copy scc binary into container: %w", err)
		}
	}
	if bins.campLegacy != "" {
		if err := container.CopyFileToContainer(ctx, bins.campLegacy, "/camp-legacy", 0o755); err != nil {
			container.Terminate(ctx)
			return nil, fmt.Errorf("failed to copy legacy camp binary into container: %w", err)
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
// locateFestSource finds the fest checkout to build the container's fest binary
// from, starting at the camp project root.
//
// fest normally sits beside camp as a sibling submodule (projects/camp,
// projects/fest), but camp is routinely developed from a worktree under
// projects/worktrees/camp/<branch>, where the sibling lookup lands on
// projects/worktrees/camp/fest and misses. Every fest-gated test then skipped,
// silently, and a green run meant nothing for that coverage. Walking the
// ancestors finds projects/fest from either layout.
//
// CAMP_TEST_FEST_SRC overrides the search for checkouts that live elsewhere.
func locateFestSource(projectRoot string) (string, error) {
	if override := os.Getenv("CAMP_TEST_FEST_SRC"); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("failed to resolve CAMP_TEST_FEST_SRC: %w", err)
		}
		if _, err := os.Stat(filepath.Join(abs, "cmd", "fest")); err != nil {
			return "", fmt.Errorf("CAMP_TEST_FEST_SRC=%s has no cmd/fest: %w", abs, err)
		}
		return abs, nil
	}

	searched := []string{}
	for dir := projectRoot; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "fest")
		searched = append(searched, candidate)
		if _, err := os.Stat(filepath.Join(candidate, "cmd", "fest")); err == nil {
			return candidate, nil
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return "", fmt.Errorf("fest source not found; searched %s (set CAMP_TEST_FEST_SRC to override)",
		strings.Join(searched, ", "))
}

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

	festRoot, err := locateFestSource(projectRoot)
	if err != nil {
		return "", err
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
