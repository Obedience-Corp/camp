// internal/buildutil/tasks/test.go
package tasks

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/buildutil/ui"
)

// TestResult tracks test results for a package
type TestResult struct {
	Package     string
	Pass        bool
	Duration    time.Duration
	HasTests    bool
	TestsPassed int
	TestsFailed int
	FailReason  string // set for process-level failures (build/timeout/setup), else ""
	FailDetail  string // captured output for a process-level failure, else ""
}

// testEvent represents a single line of go test -json output
type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

// Test runs go test on all packages
func Test(verbose bool) error {
	ui.Section("Testing Camp CLI")

	packages, err := discoverTestPackages()
	if err != nil {
		return camperrors.Newf("failed to discover test packages: %w", err)
	}

	if verbose {
		fmt.Printf("Found %d packages with tests\n", len(packages))
	}

	results := make([]TestResult, len(packages))
	total := len(packages)

	// Package test binaries are independent processes, so run them concurrently
	// across a worker pool instead of one-at-a-time. Each worker writes its own
	// results slot; only the shared progress counter and UI render are guarded.
	workers := max(min(runtime.GOMAXPROCS(0), total), 1)

	wallStart := time.Now()
	jobs := make(chan int)
	var wg sync.WaitGroup
	var completed int64
	var uiMu sync.Mutex

	for range workers {
		wg.Go(func() {
			for i := range jobs {
				pkg := packages[i]
				shortName := strings.TrimPrefix(pkg, "./")
				if shortName == "." {
					shortName = "root"
				}

				start := time.Now()
				// Run with -json to get detailed test counts. The timeout is a
				// hang-detection safety net, not a perf gate: some packages have
				// tests that legitimately run close to 30s (e.g. stale-lock wait
				// paths), and CPU/IO contention under the parallel pool can push
				// them over a tight limit. Keep generous headroom so the gate
				// stays reliable while still catching true hangs.
				cmd := exec.Command("go", goTestArgs(pkg)...)
				output, runErr := cmd.Output()
				duration := time.Since(start)

				testsPassed, testsFailed := parseTestOutput(output, verbose)
				// Fail closed on a non-zero `go test` exit. Build errors, timeouts,
				// and setup failures emit only package-level json events (empty
				// Test), which parseTestOutput ignores, so a package can exit
				// non-zero with zero counted test failures. Gating on runErr keeps
				// the dashboard from reporting those as green.
				failReason, failDetail := "", ""
				if runErr != nil && testsFailed == 0 {
					failReason, failDetail = packageFailReason(runErr, output)
				}
				results[i] = TestResult{
					Package:     shortName,
					Pass:        runErr == nil && testsFailed == 0,
					Duration:    duration,
					HasTests:    true,
					TestsPassed: testsPassed,
					TestsFailed: testsFailed,
					FailReason:  failReason,
					FailDetail:  failDetail,
				}

				done := atomic.AddInt64(&completed, 1)
				uiMu.Lock()
				ui.Progress(int(done), total, fmt.Sprintf("Testing %s", shortName))
				uiMu.Unlock()
			}
		})
	}

	for i := range packages {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	ui.ClearProgress()

	wallTime := time.Since(wallStart)

	pkgFailures := 0
	for _, r := range results {
		if !r.Pass {
			pkgFailures++
		}
		if r.FailDetail != "" {
			fmt.Fprintf(os.Stderr, "\n%s: %s\n%s\n", r.Package, r.FailReason, r.FailDetail)
		}
	}

	// Calculate totals
	var totalTime time.Duration
	totalTestsPassed := 0
	totalTestsFailed := 0
	pkgsPassed := 0

	for _, r := range results {
		totalTime += r.Duration
		totalTestsPassed += r.TestsPassed
		totalTestsFailed += r.TestsFailed
		if r.Pass {
			pkgsPassed++
		}
	}

	// Display summary - only show packages with failures
	rows := [][]string{}
	hasFailures := pkgFailures > 0

	for _, r := range results {
		// Only include packages that failed
		if !r.Pass {
			var status string
			if r.TestsFailed > 0 {
				status = fmt.Sprintf("✗ %d failed", r.TestsFailed)
			} else {
				// Package-level failure (build/timeout/setup) with no per-test fail.
				status = "✗ " + r.FailReason
			}
			if ui.ColourEnabled() {
				status = ui.Red + status + ui.Reset
			}

			rows = append(rows, []string{
				r.Package,
				status,
				fmt.Sprintf("%.2fs", r.Duration.Seconds()),
			})
		}
	}

	// Add header only if there are failures to show
	if hasFailures {
		rows = append([][]string{{"Package", "Status", "Time"}}, rows...)
	}

	// Add totals row with actual test counts
	totalTests := totalTestsPassed + totalTestsFailed
	totalStatus := fmt.Sprintf("%d/%d tests passed", totalTestsPassed, totalTests)
	if ui.ColourEnabled() {
		if totalTestsFailed > 0 {
			totalStatus = ui.Red + totalStatus + ui.Reset
		} else {
			totalStatus = ui.Green + totalStatus + ui.Reset
		}
	}

	rows = append(rows, []string{
		fmt.Sprintf("%d packages", len(results)),
		totalStatus,
		fmt.Sprintf("%.2fs (%.2fs cpu)", wallTime.Seconds(), totalTime.Seconds()),
	})

	success := pkgFailures == 0
	// Choose appropriate title based on whether there are failures
	title := "Test Summary"
	if hasFailures {
		title = "Test Failures"
	} else {
		title = "Tests Complete - All Passed"
	}

	// Use custom status messages for test results
	successMsg := fmt.Sprintf("✓ ALL %d TESTS PASSED", totalTestsPassed)
	failMsg := fmt.Sprintf("✗ %d/%d TESTS FAILED", totalTestsFailed, totalTests)
	if totalTestsFailed == 0 && pkgFailures > 0 {
		failMsg = fmt.Sprintf("✗ %d PACKAGE(S) FAILED TO BUILD OR RUN", pkgFailures)
	}

	ui.SummaryCardWithStatus(title, rows, fmt.Sprintf("%.2fs", wallTime.Seconds()), success, successMsg, failMsg)

	if pkgFailures > 0 {
		return camperrors.Newf("%d packages had test failures (%d tests failed)", pkgFailures, totalTestsFailed)
	}

	return nil
}

func goTestArgs(pkg string) []string {
	args := []string{"test", "-count=1", "-json", "-short", "-timeout", "300s"}
	return append(args, appendBuildTags(pkg)...)
}

// packageFailReason classifies a non-zero `go test` exit that produced no
// per-test failure (build error, timeout, setup failure) and returns a short
// label for the summary plus the captured detail for loud printing. stdout is
// the `-json` stream; stderr (build errors, panics) is pulled from the
// ExitError when present.
func packageFailReason(runErr error, stdout []byte) (reason, detail string) {
	detail = strings.TrimSpace(string(stdout))

	var ee *exec.ExitError
	if errors.As(runErr, &ee) && len(ee.Stderr) > 0 {
		if detail != "" {
			detail += "\n"
		}
		detail += strings.TrimSpace(string(ee.Stderr))
	}
	if detail == "" {
		detail = runErr.Error()
	}

	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "timed out") || strings.Contains(lower, "test killed"):
		reason = "timeout"
	case strings.Contains(lower, "build failed") || strings.Contains(lower, "[build failed]") ||
		strings.Contains(lower, "setup failed") || strings.Contains(lower, "[setup failed]"):
		reason = "build failed"
	default:
		reason = "exec error"
	}
	return reason, detail
}

// parseTestOutput parses go test -json output and returns pass/fail counts
func parseTestOutput(output []byte, verbose bool) (passed, failed int) {
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event testEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Only count actual test results (not package-level or sub-tests)
		if event.Test != "" && !strings.Contains(event.Test, "/") {
			switch event.Action {
			case "pass":
				passed++
			case "fail":
				failed++
				if verbose {
					fmt.Printf("  FAIL: %s\n", event.Test)
				}
			}
		}
	}

	return passed, failed
}

// discoverTestPackages finds all packages that have tests
func discoverTestPackages() ([]string, error) {
	packages, err := discoverPackages()
	if err != nil {
		return nil, err
	}

	var testPackages []string

	for _, pkg := range packages {
		// Skip integration tests directory
		if strings.Contains(pkg, "/tests/integration") {
			continue
		}

		// Check if package has test files
		cmd := exec.Command("go", goListArgs("-f", "{{.TestGoFiles}}", pkg)...)
		output, err := cmd.Output()
		if err != nil {
			continue
		}

		// If TestGoFiles is not empty array, package has tests
		if strings.TrimSpace(string(output)) != "[]" {
			testPackages = append(testPackages, pkg)
		}
	}

	return testPackages, nil
}
