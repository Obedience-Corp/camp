package tasks

import (
	"fmt"
	"strings"
	"testing"
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
