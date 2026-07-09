package festivals

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/ui"
)

func TestFestStatusCell(t *testing.T) {
	ui.SetNoColor(true)
	cases := map[string]string{
		"active":            "ACTIVE",
		"ready":             "READY",
		"planning":          "PLANNING",
		"ritual":            "RITUAL",
		"completed":         "COMPLETED",
		"dungeon/completed": "COMPLETED", // prefix stripped
		"weird":             "WEIRD",     // unknown -> muted, still rendered
	}
	for in, want := range cases {
		got := festStatusCell(in)
		if !strings.HasPrefix(strings.TrimRight(got, " "), want) {
			t.Errorf("festStatusCell(%q) = %q, want label %q", in, got, want)
		}
		if lipglossWidth := len([]rune(strings.TrimRight(got, ""))); lipglossWidth < festStatusW {
			t.Errorf("festStatusCell(%q) narrower than %d", in, festStatusW)
		}
	}
}

func TestFestProgressCell(t *testing.T) {
	ui.SetNoColor(true)
	cases := []struct {
		p    progress
		want string // substring that must appear
	}{
		{progress{Completed: 0, Total: 0}, "0/0  --%"},
		{progress{Completed: 0, Total: 7}, "0/7   0%"},
		{progress{Completed: 53, Total: 53}, "53/53 100%"},
		{progress{Completed: 19, Total: 67}, "19/67  28%"},
	}
	for _, c := range cases {
		if got := festProgressCell(c.p); !strings.Contains(got, c.want) {
			t.Errorf("festProgressCell(%+v) = %q, want substring %q", c.p, got, c.want)
		}
	}
}
