package workitem

import (
	"fmt"
	"hash/fnv"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/listview"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

var listPalette = theme.TUI()

func outputList(w io.Writer, items []wkitem.WorkItem, groupBy string) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(w, "No work items found.")
		return err
	}
	if groupBy == "" {
		groupBy = "group"
	}

	byKey := make(map[string]wkitem.WorkItem, len(items))
	for _, item := range items {
		byKey[item.Key] = item
	}
	sections := listview.Sections(wkitem.ListRows(items), groupBy)
	for i, section := range sections {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, listSectionHeader(section)); err != nil {
			return err
		}
		for _, row := range section.Rows {
			item := byKey[row.Key]
			if _, err := fmt.Fprintln(w, listRow(item)); err != nil {
				return err
			}
		}
	}
	return nil
}

func listSectionHeader(section listview.Section) string {
	label := section.Label
	if label == "" {
		label = section.Key
	}
	if label == "" {
		label = "Ungrouped"
	}
	return listGroupStyle(section.Key).Render(strings.ToUpper(label))
}

func listRow(item wkitem.WorkItem) string {
	statusText, statusStyle := listStatus(item)
	status := statusStyle.Render(padList(statusText, 7))
	priorityMark := listPriorityStyle(item.ManualPriority).Render(padList(listPriorityLabel(item.ManualPriority), 1))
	wfType := lipgloss.NewStyle().Foreground(listPalette.TextSecondary).Render(padList(string(item.WorkflowType), 8))
	age := lipgloss.NewStyle().Foreground(listPalette.TextMuted).Render(padList(listRecency(item.SortTimestamp), 4))
	title := lipgloss.NewStyle().Foreground(listPalette.TextPrimary).Render(item.Title)
	path := lipgloss.NewStyle().Foreground(listPalette.TextDim).Render(item.RelativePath)
	return fmt.Sprintf("  %s %s %s %s  %s  %s", status, priorityMark, wfType, age, title, path)
}

func listStatus(item wkitem.WorkItem) (string, lipgloss.Style) {
	if priority.EligibleForAttention(item) {
		return listAttentionLabel(item.AttentionStage), listAttentionStyle(item.AttentionStage)
	}
	return listLifecycleLabel(item.LifecycleStage), listLifecycleStyle(item.LifecycleStage)
}

func listAttentionLabel(stage string) string {
	switch stage {
	case "current":
		return "current"
	case "next":
		return "next"
	case "active":
		return "active"
	case "parked":
		return "parked"
	default:
		return "-"
	}
}

func listLifecycleLabel(stage wkitem.LifecycleStage) string {
	switch stage {
	case wkitem.LifecycleStageInbox:
		return "inbox"
	case wkitem.LifecycleStageActive:
		return "active"
	case wkitem.LifecycleStageReady:
		return "ready"
	case wkitem.LifecycleStagePlanning:
		return "plan"
	case wkitem.LifecycleStageRitual:
		return "ritual"
	case wkitem.LifecycleStageChains:
		return "chains"
	case wkitem.LifecycleStageNone, "":
		return "-"
	default:
		return string(stage)
	}
}

func listPriorityLabel(priority string) string {
	switch priority {
	case "high":
		return "H"
	case "medium":
		return "M"
	case "low":
		return "L"
	default:
		return "-"
	}
}

func listRecency(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

func listAttentionStyle(stage string) lipgloss.Style {
	switch stage {
	case "current":
		return lipgloss.NewStyle().Bold(true).Foreground(listPalette.Accent)
	case "next":
		return lipgloss.NewStyle().Bold(true).Foreground(listPalette.AccentAlt)
	case "active":
		return lipgloss.NewStyle().Foreground(listPalette.TextSecondary)
	case "parked":
		return lipgloss.NewStyle().Foreground(listPalette.TextDim)
	default:
		return lipgloss.NewStyle().Foreground(listPalette.TextDim)
	}
}

func listLifecycleStyle(stage wkitem.LifecycleStage) lipgloss.Style {
	switch stage {
	case wkitem.LifecycleStageActive:
		return lipgloss.NewStyle().Foreground(listPalette.Success)
	case wkitem.LifecycleStageReady:
		return lipgloss.NewStyle().Foreground(listPalette.Warning)
	case wkitem.LifecycleStageInbox:
		return lipgloss.NewStyle().Foreground(listPalette.TextMuted)
	case wkitem.LifecycleStagePlanning, wkitem.LifecycleStageRitual, wkitem.LifecycleStageChains:
		return lipgloss.NewStyle().Foreground(listPalette.AccentAlt)
	default:
		return lipgloss.NewStyle().Foreground(listPalette.TextDim)
	}
}

func listPriorityStyle(priority string) lipgloss.Style {
	switch priority {
	case "high":
		return lipgloss.NewStyle().Bold(true).Foreground(listPalette.Error)
	case "medium":
		return lipgloss.NewStyle().Bold(true).Foreground(listPalette.Warning)
	case "low":
		return lipgloss.NewStyle().Foreground(listPalette.TextDim)
	default:
		return lipgloss.NewStyle().Foreground(listPalette.TextDim)
	}
}

func listGroupStyle(key string) lipgloss.Style {
	if key == "" || key == "ungrouped" {
		return lipgloss.NewStyle().Bold(true).Foreground(listPalette.TextMuted)
	}
	colors := []lipgloss.TerminalColor{
		listPalette.Accent,
		listPalette.AccentAlt,
		listPalette.Success,
		listPalette.Warning,
	}
	return lipgloss.NewStyle().Bold(true).Foreground(colors[hashString(key)%uint32(len(colors))])
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func padList(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
