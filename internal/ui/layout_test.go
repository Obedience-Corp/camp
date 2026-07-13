package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"hello-world", 8, "hello..."},
		{"short", 10, "short"},
		{"abcdef", 0, ""},
		{"abcdef", -1, ""},
		{"abcdef", 3, "abc"},
		{"héllo-wörld-日本語", 8, "héllo..."},
		{"日本語テスト", 3, "日本語"},
	}
	for _, c := range cases {
		if got := Truncate(c.s, c.n); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestClampWidth_ZeroOrNegativeIsPassthrough(t *testing.T) {
	if got := ClampWidth("anything", 0); got != "anything" {
		t.Errorf("ClampWidth(_, 0) = %q, want passthrough", got)
	}
	if got := ClampWidth("anything", -5); got != "anything" {
		t.Errorf("ClampWidth(_, -5) = %q, want passthrough", got)
	}
}

func TestClampLines_ZeroWidthIsPassthrough(t *testing.T) {
	in := []string{"a", "bb", "ccc"}
	out := ClampLines(in, 0)
	if len(out) != len(in) {
		t.Fatalf("ClampLines width 0 changed length: %d -> %d", len(in), len(out))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("ClampLines width 0 altered line %d: %q -> %q", i, in[i], out[i])
		}
	}
}

func TestCapFrame_HeightAndWidth(t *testing.T) {
	in := []string{"alpha", "bravo-charlie", "delta", "echo"}
	out := CapFrame(in, 5, 2)
	if len(out) != 2 {
		t.Fatalf("CapFrame height: got %d lines, want 2: %v", len(out), out)
	}
	for i, line := range out {
		if got := lipgloss.Width(line); got > 5 {
			t.Errorf("CapFrame width: line %d width %d > 5 (%q)", i, got, line)
		}
	}
}

func TestCapFrame_ZeroMeansPassthroughLimits(t *testing.T) {
	in := []string{"one", "two", "three"}
	out := CapFrame(in, 0, 0)
	if len(out) != 3 || out[0] != "one" || out[2] != "three" {
		t.Fatalf("CapFrame(0,0) = %v, want passthrough", out)
	}
}

func TestCollapseHelp(t *testing.T) {
	full := "j/k move . g go . f filter . q quit"
	mid := "j/k . g . q"
	short := "q"
	if got := CollapseHelp(0, full, mid, short); got != full {
		t.Errorf("unknown width: got %q, want full", got)
	}
	if got := CollapseHelp(80, full, mid, short); got != full {
		t.Errorf("wide: got %q, want full", got)
	}
	if got := CollapseHelp(lipgloss.Width(mid), full, mid, short); got != mid {
		t.Errorf("mid width: got %q, want mid", got)
	}
	if got := CollapseHelp(1, full, mid, short); got != short {
		t.Errorf("tiny: got %q, want short", got)
	}
	if got := CollapseHelp(10); got != "" {
		t.Errorf("no levels: got %q, want empty", got)
	}
}

func TestFitFullscreenView_RemovesPhantomTrailingRows(t *testing.T) {
	got := FitFullscreenView("top\nmiddle\nbottom\n", 3)
	if got != "top\nmiddle\nbottom" {
		t.Fatalf("FitFullscreenView = %q, want view without trailing row", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("fullscreen view must not end with a newline")
	}
}

func TestFitFullscreenView_PreservesTopWhenContentOverflows(t *testing.T) {
	got := FitFullscreenView("top\nsecond\nthird\nbottom", 3)
	if got != "top\nsecond\nthird" {
		t.Fatalf("FitFullscreenView = %q, want top three rows", got)
	}
}

func TestFitFullscreenView_UnknownHeightOnlyTrimsTrailingRows(t *testing.T) {
	got := FitFullscreenView("top\nbottom\n\n", 0)
	if got != "top\nbottom" {
		t.Fatalf("FitFullscreenView unknown height = %q", got)
	}
}

func TestWindowRange_KeepsCursorVisible(t *testing.T) {
	for total := 1; total <= 20; total++ {
		for capacity := 1; capacity <= total+2; capacity++ {
			for cursor := 0; cursor < total; cursor++ {
				start, end := WindowRange(cursor, total, capacity)
				if start < 0 || end > total || start > end {
					t.Fatalf("total=%d cap=%d cur=%d: bad range [%d,%d)", total, capacity, cursor, start, end)
				}
				if end-start > capacity {
					t.Fatalf("total=%d cap=%d cur=%d: window %d exceeds capacity", total, capacity, cursor, end-start)
				}
				if cursor < start || cursor >= end {
					t.Fatalf("total=%d cap=%d cur=%d: cursor outside [%d,%d)", total, capacity, cursor, start, end)
				}
			}
		}
	}
}

func TestWindowRange_CapacityAtOrBeyondTotal(t *testing.T) {
	if s, e := WindowRange(0, 5, 5); s != 0 || e != 5 {
		t.Errorf("capacity == total: got [%d,%d), want [0,5)", s, e)
	}
	if s, e := WindowRange(2, 5, 99); s != 0 || e != 5 {
		t.Errorf("capacity > total: got [%d,%d), want [0,5)", s, e)
	}
}

func TestClampIdx(t *testing.T) {
	cases := []struct{ v, n, want int }{
		{0, 0, 0},
		{5, 0, 0},
		{-3, 4, 0},
		{9, 4, 3},
		{2, 4, 2},
	}
	for _, c := range cases {
		if got := ClampIdx(c.v, c.n); got != c.want {
			t.Errorf("ClampIdx(%d, %d) = %d, want %d", c.v, c.n, got, c.want)
		}
	}
}

func TestCursorGlyph(t *testing.T) {
	if got := CursorGlyph(true); got != "> " {
		t.Errorf("CursorGlyph(true) = %q, want %q", got, "> ")
	}
	if got := CursorGlyph(false); got != "  " {
		t.Errorf("CursorGlyph(false) = %q, want %q", got, "  ")
	}
}
