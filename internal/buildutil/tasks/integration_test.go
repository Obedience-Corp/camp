package tasks

import (
	"errors"
	"strings"
	"testing"
)

var (
	errExitStatus1 = errors.New("exit status 1")
	errScanner     = errors.New("token too long")
)

// The summary is the only thing most people read after a run, so its count has
// to mean what it says.
//
// It did not. `go test` exits non-zero because a test failed, and the runner
// recorded that exit as a failure of its own, so one broken test was reported
// as "2/734 tests failed" with a second row reading "go test exited: exit
// status 1". That row names nothing anyone can open, rerun, or fix, and it
// sends a reader looking for a second broken test that was never there.
func TestClassifyRunFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		scanErr       error
		waitErr       error
		testsFailed   int
		packageFailed bool
		infraPackage  bool
		want          string // "" means the run failure is not news
		wantContain   string
	}{
		{
			name:        "a clean run reports nothing",
			testsFailed: 0,
			want:        "",
		},
		{
			// The case that produced the phantom row.
			name:        "a failing test already explains the non-zero exit",
			waitErr:     errExitStatus1,
			testsFailed: 1,
			want:        "",
		},
		{
			name:        "many failing tests still explain it",
			waitErr:     errExitStatus1,
			testsFailed: 23,
			want:        "",
		},
		{
			// Nothing claimed the failure, so the exit is the only evidence
			// there is: a build error, a panic, or a timeout.
			name:        "an unexplained exit is surfaced",
			waitErr:     errExitStatus1,
			testsFailed: 0,
			wantContain: "exited without reporting a failing test",
		},
		{
			// A package-level fail event used to be dropped entirely, so a run
			// could report every test passing and still exit non-zero with
			// nothing naming the cause.
			name:          "a package failure with no failing test is surfaced",
			waitErr:       errExitStatus1,
			testsFailed:   0,
			packageFailed: true,
			wantContain:   "test package failed with no failing test",
		},
		{
			// The package always fails when a test does. That is not news on
			// top of the test itself.
			name:          "a package failure adds nothing when a test failed",
			waitErr:       errExitStatus1,
			testsFailed:   2,
			packageFailed: true,
			want:          "",
		},
		{
			// The harness printed its own infrastructure banner, which the
			// summary renders as the non-run verdict. Reporting the exit as
			// well invents a second incident out of the same one.
			name:          "an infrastructure refusal speaks for itself",
			waitErr:       errExitStatus1,
			packageFailed: true,
			infraPackage:  true,
			want:          "",
		},
		{
			// Output we could not read means the counts themselves are
			// suspect, so it is reported even when tests did fail.
			name:        "unreadable output is surfaced regardless of counts",
			scanErr:     errScanner,
			waitErr:     errExitStatus1,
			testsFailed: 5,
			wantContain: "could not read test output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyRunFailure(tt.scanErr, tt.waitErr, tt.testsFailed, tt.packageFailed, tt.infraPackage)
			if tt.wantContain != "" {
				if !strings.Contains(got, tt.wantContain) {
					t.Fatalf("classifyRunFailure() = %q, want it to mention %q",
						got, tt.wantContain)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("classifyRunFailure() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The denominator has to be the suite, not the part of it that ran.
//
// The integration harness reports infrastructure death as skips, so a run that
// dies early leaves hundreds of tests that never executed. Counting only pass
// and fail shrank the denominator to match, and a collapsed run rendered as
// "23/44 tests passed" — a healthy-looking suite with a few broken tests,
// which is the opposite of what happened.
func TestTestTally(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		passed  int
		total   int
		skipped int
		want    string
	}{
		{
			name:   "a clean run says nothing about skips",
			passed: 905,
			total:  905,
			want:   "905/905 tests passed",
		},
		{
			// The run that prompted this: 21 red rows, and every one of them
			// passes on an idle machine.
			name:    "a collapsed run cannot hide behind a shrunken denominator",
			passed:  23,
			total:   905,
			skipped: 861,
			want:    "23/905 tests passed (861 skipped)",
		},
		{
			name:    "an ordinary skip is still reported",
			passed:  896,
			total:   905,
			skipped: 8,
			want:    "896/905 tests passed (8 skipped)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := testTally(tt.passed, tt.total, tt.skipped); got != tt.want {
				t.Fatalf("testTally(%d, %d, %d) = %q, want %q",
					tt.passed, tt.total, tt.skipped, got, tt.want)
			}
		})
	}
}

// Whatever it reports, it is never a test name. The summary lists failed tests
// by name so they can be rerun; a line that cannot be rerun must not look like
// one of them.
func TestClassifyRunFailureNeverLooksLikeATestName(t *testing.T) {
	t.Parallel()

	got := classifyRunFailure(nil, errExitStatus1, 0, false, false)
	if got == "" {
		t.Fatal("an unexplained exit must be surfaced")
	}
	if strings.HasPrefix(got, "Test") {
		t.Fatalf("classifyRunFailure() = %q, which reads like a test name", got)
	}
}
