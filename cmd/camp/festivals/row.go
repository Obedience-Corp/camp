package festivals

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var festPal = theme.TUI()

var (
	festBadgeActive   = lipgloss.NewStyle().Foreground(festPal.Success)
	festBadgeReady    = lipgloss.NewStyle().Foreground(festPal.AccentAlt)
	festBadgePlanning = lipgloss.NewStyle().Foreground(festPal.Warning)
	festBadgeRitual   = lipgloss.NewStyle().Foreground(festPal.Accent)
	festBadgeMuted    = lipgloss.NewStyle().Foreground(festPal.TextMuted)

	festBarDone  = lipgloss.NewStyle().Foreground(festPal.Success)
	festBarFill  = lipgloss.NewStyle().Foreground(festPal.Accent)
	festBarEmpty = lipgloss.NewStyle().Foreground(festPal.TextMuted)
)

// festStatusW is the badge cell width. The widest label rendered is COMPLETED
// (9, static --all path only); the TUI path renders at most PLANNING (8).
const festStatusW = 10

func festStatusCell(status string) string {
	s := strings.TrimPrefix(strings.ToLower(status), "dungeon/")
	label := fmt.Sprintf("%-*s", festStatusW, strings.ToUpper(s))
	switch s {
	case "active":
		return festBadgeActive.Render(label)
	case "ready":
		return festBadgeReady.Render(label)
	case "planning":
		return festBadgePlanning.Render(label)
	case "ritual":
		return festBadgeRitual.Render(label)
	default: // completed, archived, someday, dungeon, unknown
		return festBadgeMuted.Render(label)
	}
}

const festBarW = 10

func festProgressCell(p progress) string {
	if p.Total <= 0 {
		empty := festBarEmpty.Render(strings.Repeat(".", festBarW))
		return fmt.Sprintf("[%s] %d/%d  --%%", empty, p.Completed, p.Total)
	}
	ratio := float64(p.Completed) / float64(p.Total)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * festBarW))
	fill := festBarFill
	if p.Completed >= p.Total {
		fill = festBarDone
	}
	bar := fill.Render(strings.Repeat("#", filled)) +
		festBarEmpty.Render(strings.Repeat(".", festBarW-filled))
	pct := int(math.Round(ratio * 100))
	return fmt.Sprintf("[%s] %d/%d %3d%%", bar, p.Completed, p.Total, pct)
}
