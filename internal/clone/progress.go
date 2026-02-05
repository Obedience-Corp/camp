package clone

import (
	"fmt"
	"io"
	"strings"
)

// ProgressReporter handles clone progress output.
type ProgressReporter interface {
	StartPhase(name string)
	EndPhase(name string, success bool)
	StartSubmodules(total int)
	SubmoduleProgress(current, total int, name string)
	EndSubmodules(succeeded, failed int)
	Message(msg string)
	Verbose(msg string)
}

// ConsoleReporter outputs progress to console.
type ConsoleReporter struct {
	w       io.Writer
	verbose bool
}

// NewConsoleReporter creates a console progress reporter.
func NewConsoleReporter(w io.Writer, verbose bool) *ConsoleReporter {
	return &ConsoleReporter{w: w, verbose: verbose}
}

// StartPhase announces the start of a clone phase.
func (r *ConsoleReporter) StartPhase(name string) {
	fmt.Fprintf(r.w, "%s...\n", name)
}

// EndPhase reports the completion of a clone phase.
func (r *ConsoleReporter) EndPhase(name string, success bool) {
	if success {
		fmt.Fprintf(r.w, "  ✓ %s complete\n", name)
	} else {
		fmt.Fprintf(r.w, "  ✗ %s failed\n", name)
	}
}

// StartSubmodules announces the start of submodule initialization.
func (r *ConsoleReporter) StartSubmodules(total int) {
	fmt.Fprintf(r.w, "Initializing submodules (%d total)...\n", total)
}

// SubmoduleProgress reports progress on submodule initialization.
func (r *ConsoleReporter) SubmoduleProgress(current, total int, name string) {
	bar := r.progressBar(current, total, 28)
	if r.verbose {
		fmt.Fprintf(r.w, "  [%s] %d/%d %s\n", bar, current, total, name)
	} else {
		fmt.Fprintf(r.w, "\r  [%s] %d/%d", bar, current, total)
	}
}

// EndSubmodules reports the completion of submodule initialization.
func (r *ConsoleReporter) EndSubmodules(succeeded, failed int) {
	fmt.Fprintln(r.w) // Clear progress line
	if failed == 0 {
		fmt.Fprintf(r.w, "  ✓ All submodules initialized\n")
	} else {
		fmt.Fprintf(r.w, "  ⚠ %d submodules failed to initialize\n", failed)
	}
}

// Message outputs a general message.
func (r *ConsoleReporter) Message(msg string) {
	fmt.Fprintln(r.w, msg)
}

// Verbose outputs a message only when verbose mode is enabled.
func (r *ConsoleReporter) Verbose(msg string) {
	if r.verbose {
		fmt.Fprintln(r.w, "  "+msg)
	}
}

// progressBar generates a text progress bar.
func (r *ConsoleReporter) progressBar(current, total, width int) string {
	if total == 0 {
		return strings.Repeat("-", width)
	}
	filled := (current * width) / total
	return strings.Repeat("=", filled) + strings.Repeat("-", width-filled)
}

// SilentReporter discards all progress output (for JSON mode).
type SilentReporter struct{}

// StartPhase is a no-op for silent mode.
func (r *SilentReporter) StartPhase(name string) {}

// EndPhase is a no-op for silent mode.
func (r *SilentReporter) EndPhase(name string, success bool) {}

// StartSubmodules is a no-op for silent mode.
func (r *SilentReporter) StartSubmodules(total int) {}

// SubmoduleProgress is a no-op for silent mode.
func (r *SilentReporter) SubmoduleProgress(current, total int, name string) {}

// EndSubmodules is a no-op for silent mode.
func (r *SilentReporter) EndSubmodules(succeeded, failed int) {}

// Message is a no-op for silent mode.
func (r *SilentReporter) Message(msg string) {}

// Verbose is a no-op for silent mode.
func (r *SilentReporter) Verbose(msg string) {}
