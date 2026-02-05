package clone

import (
	"bytes"
	"strings"
	"testing"
)

func TestConsoleReporter_StartPhase(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, false)

	r.StartPhase("Test phase")

	if !strings.Contains(buf.String(), "Test phase") {
		t.Errorf("StartPhase output should contain phase name")
	}
}

func TestConsoleReporter_EndPhase_Success(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, false)

	r.EndPhase("Test", true)

	if !strings.Contains(buf.String(), "✓") {
		t.Errorf("EndPhase success should contain checkmark")
	}
}

func TestConsoleReporter_EndPhase_Failure(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, false)

	r.EndPhase("Test", false)

	if !strings.Contains(buf.String(), "✗") {
		t.Errorf("EndPhase failure should contain X mark")
	}
}

func TestConsoleReporter_StartSubmodules(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, false)

	r.StartSubmodules(5)

	if !strings.Contains(buf.String(), "5 total") {
		t.Errorf("StartSubmodules should show total count")
	}
}

func TestConsoleReporter_EndSubmodules_AllSuccess(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, false)

	r.EndSubmodules(5, 0)

	if !strings.Contains(buf.String(), "✓") {
		t.Errorf("EndSubmodules with no failures should show success")
	}
}

func TestConsoleReporter_EndSubmodules_SomeFailed(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, false)

	r.EndSubmodules(3, 2)

	if !strings.Contains(buf.String(), "2 submodules failed") {
		t.Errorf("EndSubmodules should show failure count")
	}
}

func TestConsoleReporter_Verbose_Enabled(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, true)

	r.Verbose("Test message")

	if !strings.Contains(buf.String(), "Test message") {
		t.Errorf("Verbose message should appear when verbose is true")
	}
}

func TestConsoleReporter_Verbose_Disabled(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, false)

	r.Verbose("Test message")

	if buf.String() != "" {
		t.Errorf("Verbose message should not appear when verbose is false")
	}
}

func TestConsoleReporter_ProgressBar(t *testing.T) {
	var buf bytes.Buffer
	r := NewConsoleReporter(&buf, true)

	r.SubmoduleProgress(5, 10, "test-module")

	output := buf.String()
	if !strings.Contains(output, "5/10") {
		t.Errorf("Progress should show current/total")
	}
	if !strings.Contains(output, "test-module") {
		t.Errorf("Progress should show module name in verbose mode")
	}
}

func TestSilentReporter_NoOutput(t *testing.T) {
	r := &SilentReporter{}

	// All methods should be no-ops
	r.StartPhase("test")
	r.EndPhase("test", true)
	r.StartSubmodules(5)
	r.SubmoduleProgress(1, 5, "test")
	r.EndSubmodules(5, 0)
	r.Message("test")
	r.Verbose("test")

	// If we get here without panic, test passes
}
