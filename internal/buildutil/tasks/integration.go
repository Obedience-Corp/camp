// internal/buildutil/tasks/integration.go
package tasks

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

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
	FailedTests  []string // Names of failed tests
	// SuiteError is a failure of the run itself rather than of any test: a
	// build error, a panic that took the process down, a timeout. Kept apart
	// from FailedTests because it is not a test and must not be counted as
	// one.
	SuiteError string
}

const integrationTestTimeout = "15m"

// Integration runs integration tests
func Integration(verbose bool) error {
	ui.Section("Running Integration Tests")

	// Clean up any orphaned test containers from previous runs
	ui.Task("Cleaning", "orphaned test containers")
	cleanCmd := exec.Command("docker", "container", "prune", "-f", "--filter", "label=org.testcontainers=true")
	cleanCmd.Run() // Ignore errors - Docker might not be available
	ui.TaskPass()

	// Set up Docker environment for Colima compatibility
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		// Try Colima's default socket path
		colimaSocket := filepath.Join(os.Getenv("HOME"), ".colima", "default", "docker.sock")
		if _, err := os.Stat(colimaSocket); err == nil {
			os.Setenv("DOCKER_HOST", "unix://"+colimaSocket)
		}
	}
	// Override Docker socket path for Ryuk inside Colima VM
	os.Setenv("TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE", "/var/run/docker.sock")

	// Build Linux binary for Docker-based integration tests
	ui.Task("Building", "Linux binary for Docker tests")
	if err := os.MkdirAll("bin/linux", 0o755); err != nil {
		ui.TaskFail()
		return camperrors.Newf("failed to create bin/linux directory: %w", err)
	}

	cmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", "bin/linux/camp", "./cmd/camp")
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
	failures := 0

	// Run each test suite
	for i, suite := range suites {
		name := strings.TrimPrefix(suite, "tests/integration/")
		if name == "" {
			name = "tests/integration"
		}

		start := time.Now()

		var pass bool
		var testsPassed, testsFailed, testsSkipped int
		var failedTests []string
		var suiteError string
		var packageFailed bool

		dockerEnv := append(os.Environ(),
			"TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock",
		)

		if verbose {
			// In verbose mode, show output directly
			cmd := exec.Command("go", "test", "-count=1", "-v", "-tags", "integration", "-timeout", integrationTestTimeout, "./"+suite)
			cmd.Env = dockerEnv
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			ui.Progress(i+1, total, fmt.Sprintf("Testing %s", name))
			pass = cmd.Run() == nil
		} else {
			// Run with -json for real-time progress
			cmd := exec.Command("go", "test", "-count=1", "-json", "-tags", "integration", "-timeout", integrationTestTimeout, "./"+suite)
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
						passed := testsPassed
						failed := testsFailed
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

				// Track all tests (including subtests for failure reporting)
				if event.Test != "" {
					switch event.Action {
					case "run":
						if !strings.Contains(event.Test, "/") {
							currentTest = event.Test
						}
					case "pass":
						if !strings.Contains(event.Test, "/") {
							testsPassed++
						}
					case "fail":
						// Track all failed tests (including subtests)
						failedTests = append(failedTests, event.Test)
						if !strings.Contains(event.Test, "/") {
							testsFailed++
						}
					case "skip":
						if !strings.Contains(event.Test, "/") {
							testsSkipped++
						}
					}
				} else if event.Action == "fail" {
					// A fail with no test name is the package failing as a
					// whole: a panic outside a test, a TestMain that exited
					// non-zero, output arriving after a test finished.
					//
					// Dropping these is why a run could report every test
					// passing and still exit non-zero, leaving the only
					// evidence as "go test exited: exit status 1" with nothing
					// to attribute it to. The package is not a test, so it is
					// recorded as a run-level failure.
					packageFailed = true
				}
				mu.Unlock()
			}

			close(done)
			scanErr := scanner.Err()
			waitErr := cmd.Wait()

			suiteError = classifyRunFailure(scanErr, waitErr, testsFailed, packageFailed)
			pass = testsFailed == 0 && suiteError == ""
			ui.ClearProgressWithOutput()
		}

		duration := time.Since(start)

		results = append(results, IntegrationResult{
			Suite:        name,
			Pass:         pass,
			Duration:     duration,
			TestsPassed:  testsPassed,
			TestsFailed:  testsFailed,
			TestsSkipped: testsSkipped,
			FailedTests:  failedTests,
			SuiteError:   suiteError,
		})

		if !pass {
			failures++
		}
	}

	ui.ClearProgress()

	// Calculate totals
	var totalTime time.Duration
	totalTestsPassed := 0
	totalTestsFailed := 0
	totalTestsSkipped := 0
	for _, r := range results {
		totalTime += r.Duration
		totalTestsPassed += r.TestsPassed
		totalTestsFailed += r.TestsFailed
		totalTestsSkipped += r.TestsSkipped
	}
	totalTests := totalTestsPassed + totalTestsFailed + totalTestsSkipped

	// Display summary - show failed tests as individual rows
	rows := [][]string{}
	hasFailures := failures > 0

	for _, r := range results {
		if r.Pass {
			continue
		}
		// Show each failed test as a row
		for _, testName := range r.FailedTests {
			status := "✗ FAILED"
			if ui.ColourEnabled() {
				status = ui.Red + status + ui.Reset
			}
			rows = append(rows, []string{
				testName,
				status,
				"",
			})
		}
		// A run-level failure gets its own row, labelled so it cannot be
		// mistaken for a test someone could go and rerun.
		if r.SuiteError != "" {
			status := "✗ SUITE"
			if ui.ColourEnabled() {
				status = ui.Red + status + ui.Reset
			}
			rows = append(rows, []string{
				fmt.Sprintf("%s: %s", r.Suite, r.SuiteError),
				status,
				"",
			})
		}
	}

	// Add header only if there are failures to show
	if hasFailures && len(rows) > 0 {
		rows = append([][]string{{"Failed Test", "Status", ""}}, rows...)
	}

	// Add totals row
	totalStatus := testTally(totalTestsPassed, totalTests, totalTestsSkipped)
	if ui.ColourEnabled() {
		if totalTestsFailed > 0 {
			totalStatus = ui.Red + totalStatus + ui.Reset
		} else {
			totalStatus = ui.Green + totalStatus + ui.Reset
		}
	}

	rows = append(rows, []string{
		fmt.Sprintf("%d suites", len(results)),
		totalStatus,
		fmt.Sprintf("%.2fs", totalTime.Seconds()),
	})

	success := failures == 0

	// Use custom status messages for integration test results
	successMsg := fmt.Sprintf("✓ ALL %d TESTS PASSED", totalTestsPassed)
	failMsg := fmt.Sprintf("✗ %d/%d TESTS FAILED", totalTestsFailed, totalTests)

	ui.SummaryCardWithStatus("Integration Test Summary", rows, fmt.Sprintf("%.2fs", totalTime.Seconds()), success, successMsg, failMsg)

	if failures > 0 {
		return camperrors.Newf("%d integration test suites failed", failures)
	}

	return nil
}

// testTally renders the headline count most people read instead of the run.
//
// Skips are in the denominator, and called out when there are any, because
// this harness expresses infrastructure death as skips: once the container
// pool stops resetting, every later test t.Skips (see TestMain in
// tests/integration). Counting only pass and fail made the denominator shrink
// to whatever ran, so a run that collapsed after 44 of 905 tests reported
// "23/44 tests passed" beside 21 red rows. That reads as a branch that broke
// 21 tests. It was a run that never happened, and the 21 all pass on an idle
// machine.
func testTally(passed, total, skipped int) string {
	tally := fmt.Sprintf("%d/%d tests passed", passed, total)
	if skipped > 0 {
		tally += fmt.Sprintf(" (%d skipped)", skipped)
	}
	return tally
}

// classifyRunFailure decides whether a `go test` process failure is news, and
// returns the line describing it, or "" when it is not.
//
// A non-zero exit is how `go test` reports that a test failed, so recording it
// as a failure of its own double-counts every real one: a single broken test
// was summarized as "2/734 tests failed", and the extra row read "go test
// exited: exit status 1", which is not a test, cannot be looked up, and cannot
// be rerun. Anyone reading that goes looking for a second broken test that
// does not exist.
//
// The exit only carries information when no test claimed the failure. That is
// a build error, a panic that took the process down, or a timeout, and then it
// is the only evidence there is, so it has to be surfaced. Still not as a test.
func classifyRunFailure(scanErr, waitErr error, testsFailed int, packageFailed bool) string {
	switch {
	case scanErr != nil:
		return fmt.Sprintf("could not read test output: %v", scanErr)
	case packageFailed && testsFailed == 0:
		return "the test package failed with no failing test: a panic outside " +
			"a test, a TestMain that exited non-zero, or output after a test " +
			"finished. Rerun with 'just test integration-verbose' to see it."
	case waitErr != nil && testsFailed == 0:
		return fmt.Sprintf("go test exited without reporting a failing test "+
			"(build error, panic, or timeout): %v", waitErr)
	default:
		return ""
	}
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
