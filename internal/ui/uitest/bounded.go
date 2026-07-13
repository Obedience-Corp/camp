// Package uitest holds shared assertions for camp interactive view tests.
package uitest

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// AssertBounded fails if the rendered view exceeds the terminal canvas.
// w <= 0 skips width checks; h <= 0 skips height checks. Trailing newlines are
// ignored when counting rows so FitFullscreenView output compares cleanly.
func AssertBounded(t testing.TB, out string, w, h int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if h > 0 && len(lines) > h {
		t.Fatalf("%dx%d: rendered %d lines, exceeds height %d:\n%s", w, h, len(lines), h, out)
	}
	if w <= 0 {
		return
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got > w {
			t.Fatalf("%dx%d: line %d width %d exceeds terminal width %d: %q", w, h, i, got, w, ln)
		}
	}
}
