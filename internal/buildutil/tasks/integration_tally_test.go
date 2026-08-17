package tasks

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// A daemon casualty and a broken test arrive as the same "fail" event; the
// banner in the output stream is the only thing that tells them apart, and
// classification is what keeps a reader from debugging code that was fine.
func TestTallyObserveSplitsInfraFromGenuineFailures(t *testing.T) {
	t.Parallel()

	ta := newIntegrationTally()
	ta.observe(integrationTestEvent{Action: "output", Test: "TestInfraVictim",
		Output: "    helpers.go:1: INFRASTRUCTURE FAILURE (not a test failure): container exec did not complete"})
	ta.observe(integrationTestEvent{Action: "fail", Test: "TestInfraVictim"})
	ta.observe(integrationTestEvent{Action: "output", Test: "TestRealBug",
		Output: "    foo_test.go:9: expected 2, got 3"})
	ta.observe(integrationTestEvent{Action: "fail", Test: "TestRealBug"})
	ta.observe(integrationTestEvent{Action: "pass", Test: "TestFine"})

	if got := ta.infraTests; len(got) != 1 || got[0] != "TestInfraVictim" {
		t.Fatalf("infraTests = %v, want [TestInfraVictim]", got)
	}
	if got := ta.failedTests; len(got) != 1 || got[0] != "TestRealBug" {
		t.Fatalf("failedTests = %v, want [TestRealBug]", got)
	}
	if ta.testsFailed != 2 || ta.testsPassed != 1 {
		t.Fatalf("counts = %d failed / %d passed, want 2/1", ta.testsFailed, ta.testsPassed)
	}
}

// Skips are how the harness expresses infrastructure death; the tally has to
// know which skips are the latch and which are ordinary t.Skip calls.
func TestTallyCollapseDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		passed        int
		bannerSkips   int
		ordinarySkips int
		want          bool
	}{
		{
			// The 2026-08-10 incident shape.
			name:        "latch skips mean the run died",
			passed:      38,
			bannerSkips: 871,
			want:        true,
		},
		{
			name:        "a single latch skip is already conclusive",
			passed:      900,
			bannerSkips: 1,
			want:        true,
		},
		{
			name:          "a handful of ordinary skips is a healthy run",
			passed:        907,
			ordinarySkips: 8,
			want:          false,
		},
		{
			// Backstop: banner attribution can be lost to output interleaving,
			// but mass skipping is still the collapse signature.
			name:          "mass unattributed skips still read as collapse",
			passed:        100,
			ordinarySkips: 300,
			want:          true,
		},
		{
			// The absolute floor: a small suite skipping a couple of tests on
			// platform grounds is not an incident.
			name:          "a small suite with a few skips is not a collapse",
			passed:        3,
			ordinarySkips: 2,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ta := newIntegrationTally()
			for i := range tt.passed {
				ta.observe(integrationTestEvent{Action: "pass", Test: fmt.Sprintf("TestPass%d", i)})
			}
			for i := range tt.bannerSkips {
				name := fmt.Sprintf("TestLatchSkip%d", i)
				ta.observe(integrationTestEvent{Action: "output", Test: name,
					Output: "    main_test.go:1: INFRASTRUCTURE FAILURE (not a test failure): pool dead"})
				ta.observe(integrationTestEvent{Action: "skip", Test: name})
			}
			for i := range tt.ordinarySkips {
				ta.observe(integrationTestEvent{Action: "skip", Test: fmt.Sprintf("TestSkip%d", i)})
			}
			if got := ta.collapsed(); got != tt.want {
				t.Fatalf("collapsed() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Regression fixture for the 2026-08-10 incident: 38 passed, 6 daemon
// casualties, 871 latch skips, rendered as six ✗ FAILED rows under a normal
// summary. A collapsed run must never render as a table of broken tests.
func TestSummarizeCollapsedRunIsANonRunNotATestTable(t *testing.T) {
	t.Parallel()

	results := []IntegrationResult{{
		Suite:        "tests/integration",
		Pass:         false,
		TestsPassed:  38,
		TestsFailed:  6,
		TestsSkipped: 871,
		InfraTests: []string{
			"TestIntegration_WorkitemCommits_JSONSchemaVersion",
			"TestIntegration_Crit01_JSONShowsTagsAndMultiProject",
			"TestIntegration_WorkitemCommit_JSONSchemaVersion",
			"TestIntegration_WorkitemCommit_SymlinkedCampaignRoot",
			"TestDungeonMove_TriageToDocsDestination",
			"TestWorktreesClean_DryRunNoChanges",
		},
		InfraSkipped: 871,
		Collapsed:    true,
	}}

	s := summarizeIntegration(results, false)

	if s.success {
		t.Fatal("a collapsed run must not be a success")
	}
	if !strings.Contains(s.failMsg, "RUN DID NOT HAPPEN") {
		t.Fatalf("failMsg = %q, want the non-run banner", s.failMsg)
	}
	if !strings.Contains(s.failMsg, "871 tests never ran") {
		t.Fatalf("failMsg = %q, want the never-ran count", s.failMsg)
	}
	for _, row := range s.rows {
		if len(row) > 1 && row[1] == "✗ FAILED" {
			t.Fatalf("daemon casualty rendered as a broken test: %v", row)
		}
	}
	infraRows := 0
	nonRunRows := 0
	for _, row := range s.rows {
		if len(row) > 1 && row[1] == "✗ INFRA" {
			infraRows++
		}
		if len(row) > 1 && row[1] == "✗ NON-RUN" {
			nonRunRows++
		}
	}
	if infraRows != 6 {
		t.Fatalf("infra rows = %d, want 6", infraRows)
	}
	if nonRunRows != 1 {
		t.Fatalf("non-run recovery rows = %d, want exactly 1", nonRunRows)
	}
}

// A genuine failure keeps today's rendering: named row, honest count, no
// banner. Honest failure reporting must not blunt real failures.
func TestSummarizeGenuineFailureIsUnchanged(t *testing.T) {
	t.Parallel()

	results := []IntegrationResult{{
		Suite:        "tests/integration",
		Pass:         false,
		TestsPassed:  914,
		TestsFailed:  1,
		FailedTests:  []string{"TestRealBug"},
		TestsSkipped: 8,
	}}

	s := summarizeIntegration(results, false)

	if s.collapsed {
		t.Fatal("a genuine failure is not a collapse")
	}
	if want := "✗ 1/923 TESTS FAILED"; s.failMsg != want {
		t.Fatalf("failMsg = %q, want %q", s.failMsg, want)
	}
	found := false
	for _, row := range s.rows {
		if len(row) > 1 && row[0] == "TestRealBug" && row[1] == "✗ FAILED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("genuine failure row missing from rows: %v", s.rows)
	}
}

func TestLatestTelemetryRecord(t *testing.T) {
	t.Parallel()

	if got := latestTelemetryRecord(nil); got != "" {
		t.Fatalf("empty buffer = %q, want empty", got)
	}
	data := []byte("{\"execs\":1}\n{\"execs\":9976,\"pool\":4}\n")
	if got := latestTelemetryRecord(data); got != "{\"execs\":9976,\"pool\":4}" {
		t.Fatalf("latest record = %q", got)
	}
}

// A fully green run must stay green and must not mention collapse machinery.
func TestSummarizeGreenRun(t *testing.T) {
	t.Parallel()

	results := []IntegrationResult{{
		Suite:        "tests/integration",
		Pass:         true,
		TestsPassed:  907,
		TestsSkipped: 8,
	}}

	s := summarizeIntegration(results, false)

	if !s.success {
		t.Fatal("a green run must be a success")
	}
	if len(s.rows) != 1 {
		t.Fatalf("green run should render only the totals row, got %v", s.rows)
	}
}

// A run that refuses to start has no test to attach its failure to. The
// harness prints its banner as package-level output, and that has to reach the
// summary as the non-run verdict; otherwise a preflight refusal renders as an
// unattributable "the test package failed" row, which is the same lie the
// non-run banner exists to replace.
func TestTallyPackageBannerIsANonRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		output     string
		wantNonRun bool
		wantReason string
	}{
		{
			name:       "a lock refusal is a non-run",
			output:     "INFRASTRUCTURE FAILURE (not a test failure): integration suite is still locked after 30m0s by pid 4711\n",
			wantNonRun: true,
			wantReason: "pid 4711",
		},
		{
			name:       "a preflight probe refusal is a non-run",
			output:     "INFRASTRUCTURE FAILURE (not a test failure): the Docker daemon at unix:///d.sock did not answer\n",
			wantNonRun: true,
			wantReason: "did not answer",
		},
		{
			name:   "ordinary package output is not a non-run",
			output: "container pool: 6 (docker daemon reports 6 CPUs)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ta := newIntegrationTally()
			ta.observe(integrationTestEvent{Action: "output", Output: tt.output})

			if got := ta.collapsed(); got != tt.wantNonRun {
				t.Fatalf("collapsed() = %v, want %v", got, tt.wantNonRun)
			}
			if tt.wantReason != "" && !strings.Contains(ta.infraReason, tt.wantReason) {
				t.Fatalf("infraReason = %q, want it to contain %q", ta.infraReason, tt.wantReason)
			}
			// The banner is also the whole story: an exit code arriving after
			// it must not be reported as a separate suite failure.
			if got := classifyRunFailure(nil, errExitStatus1, 0, true, ta.infraPackage); tt.wantNonRun && got != "" {
				t.Fatalf("classifyRunFailure() = %q, want the banner to speak alone", got)
			}
		})
	}
}

// The first banner wins: a refusal that cascades into further infrastructure
// noise still has exactly one cause, and the summary shows exactly one row.
func TestTallyKeepsTheFirstInfraReason(t *testing.T) {
	t.Parallel()

	ta := newIntegrationTally()
	ta.observe(integrationTestEvent{Action: "output",
		Output: "INFRASTRUCTURE FAILURE (not a test failure): the daemon did not answer\n"})
	ta.observe(integrationTestEvent{Action: "output",
		Output: "INFRASTRUCTURE FAILURE (not a test failure): and then the tunnel died\n"})

	if !strings.Contains(ta.infraReason, "did not answer") {
		t.Fatalf("infraReason = %q, want the first cause", ta.infraReason)
	}
}

// A refused run has no denominator to report, so the verdict has to say the
// suite never started rather than that zero tests never ran, and the recovery
// row has to carry the harness's own reason.
func TestSummarizeRefusedRunNamesTheReason(t *testing.T) {
	t.Parallel()

	results := []IntegrationResult{{
		Suite:       "tests/integration",
		Pass:        false,
		Collapsed:   true,
		InfraReason: "INFRASTRUCTURE FAILURE (not a test failure): integration suite is still locked after 30m0s by pid 4711",
	}}

	s := summarizeIntegration(results, false)

	if s.success {
		t.Fatal("a refused run must not be a success")
	}
	if !strings.Contains(s.failMsg, "RUN DID NOT HAPPEN") {
		t.Fatalf("failMsg = %q, want the non-run banner", s.failMsg)
	}
	if !strings.Contains(s.failMsg, "never started") {
		t.Fatalf("failMsg = %q, want it to say the suite never started", s.failMsg)
	}
	found := false
	for _, row := range s.rows {
		if len(row) > 1 && row[1] == "✗ NON-RUN" {
			found = true
			if !strings.Contains(row[0], "pid 4711") {
				t.Fatalf("non-run row = %q, want the harness's own reason", row[0])
			}
		}
	}
	if !found {
		t.Fatalf("no non-run row in %v", s.rows)
	}
}

// Without a reason from the harness, the row still has to offer a recovery
// step rather than an empty cell.
func TestSummarizeCollapseFallsBackToGenericRecovery(t *testing.T) {
	t.Parallel()

	s := summarizeIntegration([]IntegrationResult{{
		Suite:        "tests/integration",
		TestsSkipped: 900,
		Collapsed:    true,
	}}, false)

	for _, row := range s.rows {
		if len(row) > 1 && row[1] == "✗ NON-RUN" && !strings.Contains(row[0], "idle machine") {
			t.Fatalf("non-run row = %q, want the generic recovery step", row[0])
		}
	}
}

// The table's heading has to match what the rows are. A refused run lists one
// finding, not a failed test, and heading that column "Failed Test" repeats in
// miniature the category error the non-run verdict exists to correct.
func TestSummarizeHeadingMatchesTheRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []IntegrationResult
		want    string
	}{
		{
			name: "a refused run heads its column with a finding",
			results: []IntegrationResult{{
				Suite: "tests/integration", Collapsed: true,
				InfraReason: "INFRASTRUCTURE FAILURE (not a test failure): the daemon did not answer",
			}},
			want: "Finding",
		},
		{
			name: "a run with a broken test still heads it with the test",
			results: []IntegrationResult{{
				Suite: "tests/integration", TestsFailed: 1, FailedTests: []string{"TestRealBug"},
			}},
			want: "Failed Test",
		},
		{
			name: "daemon casualties are named tests too",
			results: []IntegrationResult{{
				Suite: "tests/integration", TestsSkipped: 900, Collapsed: true,
				InfraTests: []string{"TestCasualty"},
			}},
			want: "Failed Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := summarizeIntegration(tt.results, false)
			if len(s.rows) == 0 {
				t.Fatal("expected rows")
			}
			if got := s.rows[0][0]; got != tt.want {
				t.Fatalf("heading = %q, want %q", got, tt.want)
			}
		})
	}
}

// The verbose lane has no JSON events, so the banner in the raw stream is the
// only thing that can say the run did not happen. It has to tell the harness's
// own refusal (column zero) from a banner inside one test's failure output
// (indented by go test), because the second is a fault a run survives.
func TestBannerWatcher(t *testing.T) {
	t.Parallel()

	const banner = "INFRASTRUCTURE FAILURE (not a test failure): the daemon did not answer"

	tests := []struct {
		name       string
		writes     []string
		wantFound  bool
		wantReason string
	}{
		{
			name:       "a package level banner is a refusal",
			writes:     []string{"container pool: 6\n" + banner + "\nThe integration run did not happen\n"},
			wantFound:  true,
			wantReason: banner,
		},
		{
			name:      "an indented banner belongs to one test",
			writes:    []string{"=== RUN   TestThing\n    helpers.go:1: " + banner + "\n--- FAIL: TestThing\n"},
			wantFound: false,
		},
		{
			name:       "a banner split across writes is still found",
			writes:     []string{"INFRASTRUCTURE FAIL", "URE (not a test failure): the daemon", " did not answer\n"},
			wantFound:  true,
			wantReason: banner,
		},
		{
			name:      "an unterminated banner line is not a refusal yet",
			writes:    []string{banner},
			wantFound: false,
		},
		{
			name:      "ordinary output carries no verdict",
			writes:    []string{"ok  \tgithub.com/Obedience-Corp/camp/tests/integration\t696.880s\n"},
			wantFound: false,
		},
		{
			name:       "the first banner wins",
			writes:     []string{banner + "\n" + "INFRASTRUCTURE FAILURE (not a test failure): and then the tunnel died\n"},
			wantFound:  true,
			wantReason: banner,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := &bannerWatcher{}
			for _, chunk := range tt.writes {
				n, err := w.Write([]byte(chunk))
				if err != nil {
					t.Fatalf("Write() error = %v", err)
				}
				if n != len(chunk) {
					t.Fatalf("Write() = %d, want %d: a tee that shortens the stream would truncate the run's output", n, len(chunk))
				}
			}
			found, reason := w.refusal()
			if found != tt.wantFound {
				t.Fatalf("refusal() = %v (%q), want %v", found, reason, tt.wantFound)
			}
			if tt.wantReason != "" && reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

// A long line without a newline must not let the watcher's buffer grow without
// limit over a twelve minute run.
func TestBannerWatcherBoundsAnUnterminatedLine(t *testing.T) {
	t.Parallel()

	w := &bannerWatcher{}
	for range 8 {
		if _, err := w.Write(bytes.Repeat([]byte("x"), maxBannerLine)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if len(w.partial) > maxBannerLine {
		t.Fatalf("buffered %d bytes, want at most %d", len(w.partial), maxBannerLine)
	}
}

// A daemon that could not be prepared is the same event as a suite that
// refused to start, and must render the same way: one non-run verdict naming
// the cause, no test rows, non-zero exit.
func TestDaemonRefusalRendersAsANonRun(t *testing.T) {
	t.Parallel()

	cause := camperrors.New("start the dedicated integration daemon (Colima profile camp-itest): boom")
	summary := summarizeIntegration([]IntegrationResult{{
		Suite:       integrationSuiteDir,
		Collapsed:   true,
		InfraReason: infraBannerMarker + " (not a test failure): " + cause.Error(),
	}}, false)

	if summary.success {
		t.Fatal("a daemon that could not be prepared is not a success")
	}
	if !strings.Contains(summary.failMsg, "RUN DID NOT HAPPEN") ||
		!strings.Contains(summary.failMsg, "never started") {
		t.Fatalf("failMsg = %q, want the non-run verdict", summary.failMsg)
	}
	nonRun := 0
	for _, row := range summary.rows {
		if len(row) > 1 && row[1] == "✗ FAILED" {
			t.Fatalf("a daemon refusal rendered as a broken test: %v", row)
		}
		if len(row) > 1 && row[1] == "✗ NON-RUN" {
			nonRun++
			if !strings.Contains(row[0], "camp-itest") {
				t.Errorf("non-run row = %q, want the cause", row[0])
			}
		}
	}
	if nonRun != 1 {
		t.Fatalf("non-run rows = %d, want exactly 1", nonRun)
	}
}
