package notice

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func withColorProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestRender_PlainWhenColorDisabled(t *testing.T) {
	withColorProfile(t, termenv.Ascii)

	var out strings.Builder
	Render(&out, []Notice{{ID: "x", Message: "campaign has drifted", Command: "camp fix it"}})

	want := "⚠ notice: campaign has drifted\n  run: camp fix it\n"
	if got := out.String(); got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRender_StyledWhenColorEnabled(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)

	var out strings.Builder
	Render(&out, []Notice{{ID: "x", Message: "campaign has drifted", Command: "camp fix it"}})

	got := out.String()
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("Render() emitted no ANSI styling: %q", got)
	}
	for _, want := range []string{"⚠", "notice:", "campaign has drifted", "run:", "camp fix it"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q in %q", want, got)
		}
	}
}

func TestRender_NoNoticesWritesNothing(t *testing.T) {
	withColorProfile(t, termenv.Ascii)

	var out strings.Builder
	Render(&out, nil)

	if got := out.String(); got != "" {
		t.Fatalf("Render(nil) = %q, want empty", got)
	}
}
