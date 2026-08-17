// internal/buildutil/tasks/integration.go
package tasks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/buildutil/itestenv"
	"github.com/Obedience-Corp/camp/internal/buildutil/ui"
)

// integrationTestEvent represents a go test -json output line
type integrationTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

// IntegrationResult tracks integration test results
type IntegrationResult struct {
	Suite       string
	Pass        bool
	Duration    time.Duration
	TestsPassed int
	TestsFailed int
	// TestsSkipped is counted because the integration harness turns
	// infrastructure death into skips: once the container pool cannot be
	// reset, every later test t.Skips rather than failing. Left uncounted, a
	// run that collapsed after 44 of 905 tests reported "23/44 tests passed",
	// which reads as a healthy suite with a few broken tests instead of a run
	// that never happened.
	TestsSkipped int
	FailedTests  []string // Names of failed tests (failures of the code under test)
	// InfraTests names tests that failed while carrying the INFRASTRUCTURE
	// FAILURE banner: casualties of the Docker daemon, not of the code under
	// test. Kept apart from FailedTests because the 2026-08-10 collapse
	// rendered six of these as ✗ FAILED rows and sent the reader debugging
	// product code that was fine.
	InfraTests []string
	// InfraSkipped counts tests skipped by the harness's infrastructure-death
	// latch: tests that never ran because the run was already dead.
	InfraSkipped int
	// Collapsed marks a suite whose numbers are not a verdict: its
	// infrastructure died partway, so the pass/fail counts describe whatever
	// happened to run, not the suite.
	Collapsed bool
	// InfraReason is the harness's own account of why the run did not happen,
	// carried up from the banner it printed. Without it the summary can only
	// offer a generic guess, and a run refused for a nameable reason (a lock
	// held by another suite, a daemon that never answered) reads as the same
	// unexplained collapse as any other.
	InfraReason string
	// SuiteError is a failure of the run itself rather than of any test: a
	// build error, a panic that took the process down, a timeout. Kept apart
	// from FailedTests because it is not a test and must not be counted as
	// one.
	SuiteError string
}

// integrationTestTimeout must stay above the healthy full-suite wall time or
// it becomes a Model-3 lie: it reports "suite failed" for "suite outgrew a
// constant". The healthy run measured 1176s (~19.6m) at 916 tests on
// 2026-08-10 while this constant still said 15m — meaning even a perfectly
// idle daemon could not finish a green run. Keep it aligned with the
// integration-verbose lane in .justfiles/test.just (30m), and check it
// against the telemetry line's wall figure when the suite grows.
const integrationTestTimeout = "30m"

const (
	// socketOverrideEnv tells Ryuk where the daemon socket lives *inside* the
	// VM, which is not where the host reaches it.
	socketOverrideEnv = "TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE"
	inVMDockerSocket  = "/var/run/docker.sock"

	integrationSuiteDir = "tests/integration"

	// maxBannerLine bounds the unterminated tail bannerWatcher will hold, so a
	// suite that prints a very long line without a newline cannot grow it
	// without limit.
	maxBannerLine = 8192
)

// infraBannerMarker is the label the integration harness stamps on every
// infrastructure fault (classifyExecOutcome and infraLatch in
// tests/integration). Matching output on it is what lets this dashboard tell
// a daemon casualty from a broken test.
const infraBannerMarker = "INFRASTRUCTURE FAILURE"

// Integration runs integration tests
func Integration(ctx context.Context, verbose bool) error {
	runStart := time.Now()
	ui.Section("Running Integration Tests")

	if err := prepareDaemon(ctx); err != nil {
		return reportDaemonRefusal(runStart, err)
	}

	// Prune orphans on the resolved daemon, after the daemon is chosen: this
	// used to run against whatever DOCKER_HOST happened to say, which is the
	// shared daemon other people's containers live on.
	ui.Task("Cleaning", "orphaned test containers")
	cleanCmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f", "--filter", "label=org.testcontainers=true")
	cleanCmd.Run() // Ignore errors - Docker might not be available
	ui.TaskPass()

	// Build Linux binary for Docker-based integration tests
	ui.Task("Building", "Linux binary for Docker tests")
	if err := os.MkdirAll("bin/linux", 0o755); err != nil {
		ui.TaskFail()
		return camperrors.Newf("failed to create bin/linux directory: %w", err)
	}

	cmd := exec.CommandContext(ctx, "go", "build", "-ldflags", "-s -w", "-o", "bin/linux/camp", "./cmd/camp")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH)
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		ui.TaskFail()
		return camperrors.Newf("failed to build Linux binary: %w", err)
	}
	ui.TaskPass()

	suites, err := discoverIntegrationSuites()
	if err != nil {
		return camperrors.Newf("failed to discover integration test suites: %w", err)
	}

	if len(suites) == 0 {
		ui.Status("No integration tests found", true)
		return nil
	}

	if verbose {
		fmt.Printf("Found %d integration test suites\n", len(suites))
	}

	results := make([]IntegrationResult, 0, len(suites))
	total := len(suites)

	// Run each test suite
	for i, suite := range suites {
		name := strings.TrimPrefix(suite, integrationSuiteDir+"/")
		if name == "" {
			name = integrationSuiteDir
		}

		start := time.Now()

		var pass bool
		var suiteError string
		tally := newIntegrationTally()

		dockerEnv := append(os.Environ(), socketOverrideEnv+"="+inVMDockerSocket)

		if verbose {
			// In verbose mode, show output directly. There are no JSON events
			// to classify here, so the harness's own banner is the only signal
			// that the run did not happen; without watching for it, a refused
			// verbose run renders as a bare non-zero exit.
			cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-v", "-tags", "integration", "-timeout", integrationTestTimeout, "./"+suite)
			cmd.Env = dockerEnv
			watcher := &bannerWatcher{}
			cmd.Stdout = io.MultiWriter(os.Stdout, watcher)
			cmd.Stderr = io.MultiWriter(os.Stderr, watcher)
			ui.Progress(i+1, total, fmt.Sprintf("Testing %s", name))
			pass = cmd.Run() == nil
			if refused, reason := watcher.refusal(); refused {
				tally.infraPackage = true
				tally.infraReason = reason
				pass = false
			}
		} else {
			// Run with -json for real-time progress
			cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-json", "-tags", "integration", "-timeout", integrationTestTimeout, "./"+suite)
			cmd.Env = dockerEnv
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return camperrors.Newf("failed to create stdout pipe: %w", err)
			}

			if err := cmd.Start(); err != nil {
				return camperrors.Newf("failed to start test: %w", err)
			}

			// Track state for progress display
			var currentTest string
			var currentOutput string
			var mu sync.Mutex

			// Spinner characters for visual feedback
			spinnerChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			spinnerIdx := 0

			// Print initial two lines (will be updated in place)
			fmt.Println("  → Starting...")
			fmt.Printf("[%d/%d] ⠋ Starting... 0s", i+1, total)

			// Start a goroutine to update elapsed time
			done := make(chan bool)
			go func() {
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-done:
						return
					case <-ticker.C:
						mu.Lock()
						elapsed := time.Since(start).Seconds()
						testName := currentTest
						output := currentOutput
						passed := tally.testsPassed
						failed := tally.testsFailed
						mu.Unlock()

						// Cycle spinner
						spinner := spinnerChars[spinnerIdx%len(spinnerChars)]
						spinnerIdx++

						status := fmt.Sprintf("%d✓", passed)
						if failed > 0 {
							status += fmt.Sprintf(" %d✗", failed)
						}

						var progressLine string
						if testName != "" {
							progressLine = fmt.Sprintf("%s %s (%s) %.0fs", spinner, testName, status, elapsed)
						} else {
							progressLine = fmt.Sprintf("%s Starting... %.0fs", spinner, elapsed)
						}

						if output == "" {
							output = "waiting for output..."
						}
						ui.ProgressWithOutput(i+1, total, output, progressLine)
					}
				}
			}()

			// Parse JSON output in real-time
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				var event integrationTestEvent
				if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
					continue
				}

				mu.Lock()
				// Capture output lines (strip newlines for display)
				if event.Action == "output" && event.Output != "" {
					trimmed := strings.TrimSpace(event.Output)
					// Show subtest runs and actual test output (not just framework markers)
					if strings.HasPrefix(trimmed, "=== RUN") {
						// Show subtest being run
						currentOutput = strings.TrimPrefix(trimmed, "=== RUN   ")
					} else if trimmed != "" && !strings.HasPrefix(trimmed, "---") && !strings.HasPrefix(trimmed, "PASS") && !strings.HasPrefix(trimmed, "FAIL") {
						// Show actual test output
						currentOutput = trimmed
					}
				}

				if event.Test != "" && event.Action == "run" && !strings.Contains(event.Test, "/") {
					currentTest = event.Test
				}
				tally.observe(event)
				mu.Unlock()
			}

			close(done)
			scanErr := scanner.Err()
			waitErr := cmd.Wait()

			suiteError = classifyRunFailure(scanErr, waitErr, tally.testsFailed, tally.packageFailed, tally.infraPackage)
			// A collapsed suite is not a pass even when nothing that ran
			// failed: most of it never ran.
			pass = tally.testsFailed == 0 && suiteError == "" && !tally.collapsed()
			ui.ClearProgressWithOutput()
		}

		duration := time.Since(start)

		results = append(results, IntegrationResult{
			Suite:        name,
			InfraReason:  tally.infraReason,
			Pass:         pass,
			Duration:     duration,
			TestsPassed:  tally.testsPassed,
			TestsFailed:  tally.testsFailed,
			TestsSkipped: tally.testsSkipped,
			FailedTests:  tally.failedTests,
			InfraTests:   tally.infraTests,
			InfraSkipped: tally.infraSkipped,
			Collapsed:    tally.collapsed(),
			SuiteError:   suiteError,
		})
	}

	ui.ClearProgress()

	s := summarizeIntegration(results, ui.ColourEnabled())
	ui.SummaryCardWithStatus("Integration Test Summary", s.rows,
		fmt.Sprintf("%.2fs", s.totalTime.Seconds()), s.success, s.successMsg, s.failMsg)
	surfaceTelemetry(runStart)

	if !s.success {
		if s.collapsed {
			// Same words as the card: a reader comparing the two must not have
			// to work out whether they describe one event or two.
			if s.neverRan == 0 {
				return camperrors.New(
					"integration run did not happen: infrastructure failure (the suite never started)")
			}
			return camperrors.Newf(
				"integration run did not happen: infrastructure failure (%d tests never ran)",
				s.neverRan)
		}
		return camperrors.Newf("%d integration test suites failed", s.failedSuites)
	}

	return nil
}

// prepareDaemon chooses the Docker daemon this run uses and publishes it, so
// the test binary inherits the decision instead of making its own.
//
// The suite used to run wherever DOCKER_HOST already pointed, which on a
// development machine is the general-purpose Colima profile shared with
// whatever else is running. Its container pool then sized itself as if it
// owned that VM. Choosing the daemon here, out loud, is what makes the pool's
// capacity assumption true rather than hopeful.
func prepareDaemon(ctx context.Context) error {
	resolution, err := itestenv.Resolve(ctx, itestenv.Options{AutoStart: true, Out: os.Stdout})
	if err != nil {
		return camperrors.Wrap(err, "resolve the integration Docker daemon")
	}
	if resolution.Source == itestenv.SourceFallback {
		ui.Warning(resolution.Line())
	} else {
		fmt.Printf("  %s\n", resolution.Line())
	}
	if resolution.DockerHost != "" {
		if err := os.Setenv(itestenv.DockerHostVar, resolution.DockerHost); err != nil {
			return camperrors.Wrapf(err, "publish %s for the integration run", itestenv.DockerHostVar)
		}
	}
	if err := os.Setenv(socketOverrideEnv, inVMDockerSocket); err != nil {
		return camperrors.Wrapf(err, "publish %s for the integration run", socketOverrideEnv)
	}
	return nil
}

// reportDaemonRefusal renders a daemon that could not be prepared in the same
// vocabulary the suite uses when it refuses one itself: a single non-run
// verdict with the cause, and no test rows. The refusal is the same event
// whether it is the runner or the test binary that notices it, so it must not
// read as two different kinds of failure.
func reportDaemonRefusal(runStart time.Time, cause error) error {
	summary := summarizeIntegration([]IntegrationResult{{
		Suite:       integrationSuiteDir,
		Collapsed:   true,
		InfraReason: infraBannerMarker + " (not a test failure): " + cause.Error(),
	}}, ui.ColourEnabled())
	ui.SummaryCardWithStatus("Integration Test Summary", summary.rows,
		fmt.Sprintf("%.2fs", time.Since(runStart).Seconds()),
		summary.success, summary.successMsg, summary.failMsg)
	return camperrors.Newf(
		"integration run did not happen: infrastructure failure (the suite never started): %w. "+
			"Repair the daemon with '%s' or inspect it with '%s'",
		cause, itestenv.StartCommand, itestenv.DoctorCommand)
}

// bannerWatcher tees a stream while watching for the harness's package-level
// infrastructure banner.
//
// It matches only at the start of a line. `go test -v` indents everything a
// test prints, so an indented banner belongs to one test's failure (a single
// member-local fault the run can survive), while a banner at column zero is
// the harness saying the run itself did not happen. Treating the two alike
// would turn one unlucky container into a non-run.
type bannerWatcher struct {
	mu      sync.Mutex
	partial []byte
	reason  string
}

func (w *bannerWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.reason != "" {
		return len(p), nil
	}
	w.partial = append(w.partial, p...)
	for {
		end := bytes.IndexByte(w.partial, '\n')
		if end < 0 {
			break
		}
		line := string(w.partial[:end])
		w.partial = w.partial[end+1:]
		if strings.HasPrefix(line, infraBannerMarker) {
			w.reason = strings.TrimSpace(line)
			w.partial = nil
			return len(p), nil
		}
	}
	if len(w.partial) > maxBannerLine {
		// Keep the head: the banner starts its line, so that is the part worth
		// holding on to.
		w.partial = w.partial[:maxBannerLine]
	}
	return len(p), nil
}

// refusal reports the banner line, if the stream carried one.
func (w *bannerWatcher) refusal() (bool, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reason != "", w.reason
}

// surfaceTelemetry prints the harness's capacity record for this run beneath
// the summary card. The test binary emits the same numbers on stderr, but in
// -json mode this runner discards binary stderr, so the JSONL history file
// (written by reportExecTelemetry in tests/integration) is the channel that
// survives. Freshness-gated on the file's mtime so a run that died before
// TestMain's report cannot resurface a stale record as its own.
func surfaceTelemetry(runStart time.Time) {
	path := filepath.Join("out", "itest-history.jsonl")
	info, err := os.Stat(path)
	if err != nil || info.ModTime().Before(runStart) {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if line := latestTelemetryRecord(data); line != "" {
		fmt.Printf("  telemetry: %s\n", line)
	}
}

// latestTelemetryRecord returns the last non-empty line of a JSONL history
// buffer. Pure so the trailing-newline and empty-file cases stay tested.
func latestTelemetryRecord(data []byte) string {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// discoverIntegrationSuites finds all integration test directories
func discoverIntegrationSuites() ([]string, error) {
	var suites []string

	// Check if tests/integration directory exists
	if _, err := os.Stat("tests/integration"); os.IsNotExist(err) {
		return suites, nil
	}

	// First check if tests/integration itself has test files (flat structure)
	matches, _ := filepath.Glob("tests/integration/*_test.go")
	if len(matches) > 0 {
		suites = append(suites, "tests/integration")
	}

	// Also walk for subdirectories with tests
	err := filepath.Walk("tests/integration", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Look for subdirectories with _test.go files
		if info.IsDir() && path != "tests/integration" {
			// Check if directory has test files
			subMatches, _ := filepath.Glob(filepath.Join(path, "*_test.go"))
			if len(subMatches) > 0 {
				suites = append(suites, path)
			}
		}

		return nil
	})

	return suites, err
}
