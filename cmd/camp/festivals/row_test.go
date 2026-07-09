package festivals

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

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

func TestFestColumns_DropOrder(t *testing.T) {
	// wide: everything on
	if n, b, p := festColumns(80); !b || !p || n <= 0 {
		t.Errorf("wide: got name=%d badge=%v prog=%v, want all on", n, b, p)
	}
	// medium: progress dropped, badge kept
	if _, b, p := festColumns(festNameMin + 2 + festStatusW + 1); !b || p {
		t.Errorf("medium: want badge on, progress off; got badge=%v prog=%v", b, p)
	}
	// narrow: name only
	if _, b, p := festColumns(festNameMin); b || p {
		t.Errorf("narrow: want name only; got badge=%v prog=%v", b, p)
	}
}

func TestFestRow_NeverExceedsWidth(t *testing.T) {
	ui.SetNoColor(true)
	it := festivalItem{Festival: "festival-app-read-scaling-FA0019", Status: "active", Progress: progress{53, 53}}
	for cw := 10; cw <= 120; cw++ {
		if w := lipgloss.Width(festRow(it, cw, false)); w > cw {
			t.Errorf("festRow at cw=%d rendered width %d > cw", cw, w)
		}
	}
}
