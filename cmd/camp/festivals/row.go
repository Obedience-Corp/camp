package festivals

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/ui"
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

	festNameStyle = lipgloss.NewStyle().Foreground(festPal.TextPrimary)
	festSelStyle  = lipgloss.NewStyle().Foreground(festPal.Accent).Bold(true)

	festOrgHeader      = lipgloss.NewStyle().Foreground(festPal.AccentAlt).Bold(true)
	festCampaignHeader = lipgloss.NewStyle().Foreground(festPal.Accent)
)

const (
	festNameMax = 40
	festNameMin = 8
	// festProgressW is a conservative upper bound for the progress cell's
	// width: "[##########] nnn/nnn 100%" (3-digit completed/total). Actual
	// width varies with digit count, so festRow still hard-clamps to cw.
	festProgressW = 25
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

func festColumns(cw int) (nameW int, showBadge, showProgress bool) {
	// full: name + 2 gap + badge + 2 gap + progress
	if cw >= festNameMin+2+festStatusW+2+festProgressW {
		return min(cw-2-festStatusW-2-festProgressW, festNameMax), true, true
	}
	// drop progress: name + 2 gap + badge
	if cw >= festNameMin+2+festStatusW {
		return min(cw-2-festStatusW, festNameMax), true, false
	}
	// name only
	return max(min(cw, festNameMax), 1), false, false
}

func festRow(it festivalItem, cw int, selected bool) string {
	nameW, showBadge, showProgress := festColumns(cw)
	cell := fmt.Sprintf("%-*s", nameW, ui.Truncate(it.Festival, nameW))
	if selected {
		cell = festSelStyle.Render(cell)
	} else {
		cell = festNameStyle.Render(cell)
	}
	row := cell
	if showBadge {
		row += "  " + festStatusCell(it.Status)
	}
	if showProgress {
		row += "  " + festProgressCell(it.Progress)
	}
	// festProgressW is an approximation of the progress cell's width; the
	// actual width varies with digit count, so hard-clamp as a final guard
	// (mirrors listTUIModel.frame's use of ui.ClampWidth in list_tui_view.go).
	return ui.ClampWidth(row, cw)
}
