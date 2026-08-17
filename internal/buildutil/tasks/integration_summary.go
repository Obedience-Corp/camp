// internal/buildutil/tasks/integration_summary.go
package tasks

import (
	"fmt"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/buildutil/ui"
)

// integrationTally reduces the go test -json event stream for one suite into
// the counts and classifications the summary renders. Kept apart from the
// display loop so its behavior is testable without a terminal or a daemon.
type integrationTally struct {
	testsPassed, testsFailed, testsSkipped int
	failedTests                            []string // failures of the code under test (rerunnable)
	infraTests                             []string // failures carrying the infrastructure banner
	infraSkipped                           int
	packageFailed                          bool
	// infraPackage records an infrastructure banner that belongs to no test:
	// the harness refusing to start, or dying outside a test. A preflight
	// refusal has no failing test to attach itself to, and without this it
	// would render as an unattributable suite error.
	infraPackage bool
	infraReason  string
	sawBanner    map[string]bool
}

func newIntegrationTally() *integrationTally {
	return &integrationTally{sawBanner: make(map[string]bool)}
}

func (ta *integrationTally) observe(event integrationTestEvent) {
	if event.Action == "output" && strings.Contains(event.Output, infraBannerMarker) {
		if event.Test != "" {
			ta.sawBanner[event.Test] = true
		} else if !ta.infraPackage {
			ta.infraPackage = true
			ta.infraReason = strings.TrimSpace(event.Output)
		}
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
	if ta.infraSkipped > 0 || ta.infraPackage {
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
	infraReason  string
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
			if s.infraReason == "" {
				s.infraReason = r.InfraReason
			}
		}
	}
	totalTests := totalPassed + totalFailed + totalSkipped

	paint := func(label, colourCode string) string {
		if colour {
			return colourCode + label + ui.Reset
		}
		return label
	}

	namesATest := false
	for _, r := range results {
		if r.Pass {
			continue
		}
		if len(r.FailedTests) > 0 || len(r.InfraTests) > 0 {
			namesATest = true
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
		s.rows = append(s.rows, []string{collapseRow(s.infraReason), paint("✗ NON-RUN", ui.Red), ""})
	}

	if len(s.rows) > 0 {
		// A refused run has no failed test to head a column with, and calling
		// its one row a failed test is the same category error the non-run
		// verdict exists to correct.
		heading := "Finding"
		if namesATest {
			heading = "Failed Test"
		}
		s.rows = append([][]string{{heading, "Status", ""}}, s.rows...)
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
		if s.neverRan == 0 {
			s.failMsg = "✗ RUN DID NOT HAPPEN - INFRASTRUCTURE FAILURE (the suite never started)"
		} else {
			s.failMsg = fmt.Sprintf("✗ RUN DID NOT HAPPEN - INFRASTRUCTURE FAILURE (%d tests never ran)", s.neverRan)
		}
	} else {
		s.failMsg = fmt.Sprintf("✗ %d/%d TESTS FAILED", totalFailed, totalTests)
	}
	return s
}

// collapseRow is the one line under the banner that says what to do next. The
// harness's own account is preferred when it has one: "another suite holds the
// lock" and "the daemon never answered" have different recovery steps, and
// only the harness knows which happened.
func collapseRow(reason string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}
	return "Docker daemon out of headroom - rerun on an idle machine, or lower CAMP_TEST_POOL_SIZE"
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
func classifyRunFailure(scanErr, waitErr error, testsFailed int, packageFailed, infraPackage bool) string {
	switch {
	case infraPackage:
		// The harness already said what happened, in its own words, and the
		// summary renders that as the non-run banner. Adding "the test package
		// failed" beside it invents a second incident.
		return ""
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
