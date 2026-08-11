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

// infraBannerMarker is the label the integration harness stamps on every
// infrastructure fault (classifyExecOutcome and infraLatch in
// tests/integration). Matching output on it is what lets this dashboard tell
// a daemon casualty from a broken test.
const infraBannerMarker = "INFRASTRUCTURE FAILURE"

// Integration runs integration tests
func Integration(verbose bool) error {
	runStart := time.Now()
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

	// Run each test suite
	for i, suite := range suites {
		name := strings.TrimPrefix(suite, "tests/integration/")
		if name == "" {
			name = "tests/integration"
		}

		start := time.Now()

		var pass bool
		var suiteError string
		tally := newIntegrationTally()

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

			suiteError = classifyRunFailure(scanErr, waitErr, tally.testsFailed, tally.packageFailed)
			// A collapsed suite is not a pass even when nothing that ran
			// failed: most of it never ran.
			pass = tally.testsFailed == 0 && suiteError == "" && !tally.collapsed()
			ui.ClearProgressWithOutput()
		}

		duration := time.Since(start)

		results = append(results, IntegrationResult{
			Suite:        name,
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
			return camperrors.Newf(
				"integration run did not happen: infrastructure failure (%d tests never ran)",
				s.neverRan)
		}
		return camperrors.Newf("%d integration test suites failed", s.failedSuites)
	}

	return nil
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

// integrationTally reduces the go test -json event stream for one suite into
// the counts and classifications the summary renders. Kept apart from the
// display loop so its behavior is testable without a terminal or a daemon.
type integrationTally struct {
	testsPassed, testsFailed, testsSkipped int
	failedTests                            []string // failures of the code under test (rerunnable)
	infraTests                             []string // failures carrying the infrastructure banner
	infraSkipped                           int
	packageFailed                          bool
	sawBanner                              map[string]bool
}

func newIntegrationTally() *integrationTally {
	return &integrationTally{sawBanner: make(map[string]bool)}
}

func (ta *integrationTally) observe(event integrationTestEvent) {
	if event.Action == "output" && event.Test != "" &&
		strings.Contains(event.Output, infraBannerMarker) {
		ta.sawBanner[event.Test] = true
	}

	if event.Test == "" {
		// A fail with no test name is the package failing as a whole: a
		// panic outside a test, a TestMain that exited non-zero, output
		// arriving after a test finished.
		//
		// Dropping these is why a run could report every test passing and
		// still exit non-zero, leaving the only evidence as "go test exited:
		// exit status 1" with nothing to attribute it to. The package is not
		// a test, so it is recorded as a run-level failure.
		if event.Action == "fail" {
			ta.packageFailed = true
		}
		return
	}

	topLevel := !strings.Contains(event.Test, "/")
	switch event.Action {
	case "pass":
		if topLevel {
			ta.testsPassed++
		}
	case "fail":
		// Track all failed tests (including subtests), split by whether the
		// failure carried the infrastructure banner.
		if ta.sawBanner[event.Test] {
			ta.infraTests = append(ta.infraTests, event.Test)
		} else {
			ta.failedTests = append(ta.failedTests, event.Test)
		}
		if topLevel {
			ta.testsFailed++
		}
	case "skip":
		if topLevel {
			ta.testsSkipped++
			if ta.sawBanner[event.Test] {
				ta.infraSkipped++
			}
		}
	}
}

func (ta *integrationTally) total() int {
	return ta.testsPassed + ta.testsFailed + ta.testsSkipped
}

// collapsed reports whether this suite's numbers are a verdict or a non-run.
// Any latch-driven skip means the harness itself declared the run dead. The
// percentage backstop catches a collapse whose banner never got attributed
// (e.g. output interleaving): mass skipping is this harness's
// infrastructure-death signature, while legitimate skips are a handful
// (currently 8 of ~915). The absolute floor keeps a small suite with a
// couple of ordinary skips from tripping it.
func (ta *integrationTally) collapsed() bool {
	if ta.infraSkipped > 0 {
		return true
	}
	return ta.testsSkipped >= 10 && ta.testsSkipped*5 > ta.total()
}

// runSummary is the fully-rendered verdict for a set of suite results.
type runSummary struct {
	rows         [][]string
	totalTime    time.Duration
	success      bool
	successMsg   string
	failMsg      string
	collapsed    bool
	neverRan     int
	failedSuites int
}

// summarizeIntegration reduces per-suite results into the final card. Pure so
// the property that matters most stays testable: a collapsed run must never
// render as a table of broken tests. The 2026-08-10 incident rendered six
// daemon casualties as six ✗ FAILED rows over an 871-test skip, and the
// reader went debugging product code that was fine.
func summarizeIntegration(results []IntegrationResult, colour bool) runSummary {
	var s runSummary
	totalPassed, totalFailed, totalSkipped := 0, 0, 0
	for _, r := range results {
		s.totalTime += r.Duration
		totalPassed += r.TestsPassed
		totalFailed += r.TestsFailed
		totalSkipped += r.TestsSkipped
		if !r.Pass {
			s.failedSuites++
		}
		if r.Collapsed {
			s.collapsed = true
			s.neverRan += r.TestsSkipped
		}
	}
	totalTests := totalPassed + totalFailed + totalSkipped

	paint := func(label, colourCode string) string {
		if colour {
			return colourCode + label + ui.Reset
		}
		return label
	}

	for _, r := range results {
		if r.Pass {
			continue
		}
		for _, testName := range r.FailedTests {
			s.rows = append(s.rows, []string{testName, paint("✗ FAILED", ui.Red), ""})
		}
		// Daemon casualties are listed for completeness but labelled so
		// nobody debugs the code they name.
		for _, testName := range r.InfraTests {
			s.rows = append(s.rows, []string{testName, paint("✗ INFRA", ui.Yellow), ""})
		}
		// A run-level failure gets its own row, labelled so it cannot be
		// mistaken for a test someone could go and rerun.
		if r.SuiteError != "" {
			s.rows = append(s.rows, []string{
				fmt.Sprintf("%s: %s", r.Suite, r.SuiteError), paint("✗ SUITE", ui.Red), ""})
		}
	}
	if s.collapsed {
		s.rows = append(s.rows, []string{
			"Docker daemon out of headroom - rerun on an idle machine, or lower CAMP_TEST_POOL_SIZE",
			paint("✗ NON-RUN", ui.Red), ""})
	}

	if len(s.rows) > 0 {
		s.rows = append([][]string{{"Failed Test", "Status", ""}}, s.rows...)
	}

	totalStatus := testTally(totalPassed, totalTests, totalSkipped)
	if colour {
		if totalFailed > 0 || s.collapsed {
			totalStatus = ui.Red + totalStatus + ui.Reset
		} else {
			totalStatus = ui.Green + totalStatus + ui.Reset
		}
	}
	s.rows = append(s.rows, []string{
		fmt.Sprintf("%d suites", len(results)),
		totalStatus,
		fmt.Sprintf("%.2fs", s.totalTime.Seconds()),
	})

	s.success = s.failedSuites == 0 && !s.collapsed
	s.successMsg = fmt.Sprintf("✓ ALL %d TESTS PASSED", totalPassed)
	if s.collapsed {
		// The headline of a daemon incident is the incident, never a test
		// table: these numbers describe a run that did not happen.
		s.failMsg = fmt.Sprintf("✗ RUN DID NOT HAPPEN - INFRASTRUCTURE FAILURE (%d tests never ran)", s.neverRan)
	} else {
		s.failMsg = fmt.Sprintf("✗ %d/%d TESTS FAILED", totalFailed, totalTests)
	}
	return s
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
