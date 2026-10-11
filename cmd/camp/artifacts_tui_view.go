package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func artifactDisplay(s string) string {
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return strconv.QuoteToGraphic(s)
	}
	return s
}

func (m artifactModel) View() string {
	if m.quitting {
		return ""
	}
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	pal := theme.TUI()
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(pal.TextMuted)
	selected := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	fit := func(s string, n int) string { return ansi.Truncate(s, max(1, n), "…") }
	var size int64
	for _, f := range m.all {
		size += f.Size
	}
	heading := title.Render("Artifacts") + "  " + muted.Render(fmt.Sprintf("%s files · %s", ui.FormatCount(len(m.all)), ui.FormatBytes(size)))
	if h < 10 || w < 24 {
		return strings.Join(ui.CapFrame([]string{heading, "Resize to browse artifacts", "q quit"}, w, h), "\n")
	}
	lines := []string{fit(heading, w), muted.Render(fit("Outside git · "+artifactDisplay(filepath.Base(m.root)), w)), ""}
	listW := w
	split := w >= 96
	if split {
		listW = w * 3 / 5
	}
	rows := h - 10
	if m.status != "" {
		rows--
	}
	rows = max(1, rows)
	start, end := ui.WindowRange(m.cursor, len(m.visible), rows)
	left := make([]string, rows)
	if m.loading {
		left[0] = muted.Render("Loading artifacts…")
	} else if len(m.visible) == 0 {
		if len(m.all) == 0 {
			left[0] = "No local artifact files"
		} else {
			left[0] = "No matching artifacts"
		}
	} else {
		for i := start; i < end; i++ {
			f := m.visible[i]
			size := ui.FormatBytes(f.Size)
			pathW := listW - lipgloss.Width(size) - 5
			path := fit(artifactDisplay(filepath.Base(f.Path)), pathW)
			row := ui.CursorGlyph(i == m.cursor) + path + strings.Repeat(" ", max(1, pathW-lipgloss.Width(path)+2)) + size
			if i == m.cursor {
				row = selected.Render(row)
			}
			left[i-start] = fit(row, listW)
		}
	}
	if split {
		details := m.artifactDetails(w-listW-5, rows)
		for i := range left {
			left[i] = lipgloss.NewStyle().Width(listW).Render(left[i]) + muted.Render("  │  ") + details[i]
		}
	}
	lines = append(lines, left...)
	lines = append(lines, "")
	if !split && len(m.visible) > 0 {
		detail := strings.Split(ansi.Hardwrap(artifactDisplay(m.visible[m.cursor].Path), w, false), "\n")
		limit := 1
		if h >= 12 {
			limit = 3
		}
		for _, line := range detail[:min(len(detail), limit)] {
			lines = append(lines, muted.Render(line))
		}
	}
	if split {
		lines = append(lines, "")
	}
	if m.filtering {
		lines = append(lines, fit(m.input.View(), w))
	} else if m.input.Value() != "" {
		lines = append(lines, muted.Render(fit("Filter: "+artifactDisplay(m.input.Value()), w)))
	} else {
		lines = append(lines, muted.Render(fit("/ Filter by name or folder", w)))
	}
	count := fmt.Sprintf("%d–%d of %s", start+1, end, ui.FormatCount(len(m.visible)))
	if len(m.visible) == 0 {
		count = "0 files"
	}
	lines = append(lines, muted.Render(fit(count, w)))
	if m.status != "" {
		style := lipgloss.NewStyle().Foreground(pal.Success)
		if m.statusErr {
			style = style.Foreground(pal.Error)
		}
		lines = append(lines, style.Render(fit(artifactDisplay(m.status), w)))
	}
	help := ui.CollapseHelp(w, "↑↓ move  enter open  g go to folder  y copy path  / filter  r refresh  q quit", "↑↓ move  enter open  g folder  / filter  q quit", "o open  g go  q quit")
	if m.filtering {
		help = ui.CollapseHelp(w, "Type to filter  enter apply  esc clear  ctrl+c quit", "enter apply  esc clear")
	}
	lines = append(lines, muted.Render(fit(help, w)))
	return strings.Join(ui.CapFrame(lines, w, h), "\n")
}

func (m artifactModel) artifactDetails(w, rows int) []string {
	out := make([]string, rows)
	if len(m.visible) == 0 {
		return out
	}
	f := m.visible[m.cursor]
	pal := theme.TUI()
	label := lipgloss.NewStyle().Foreground(pal.TextMuted)
	value := lipgloss.NewStyle().Foreground(pal.TextPrimary)
	kind := strings.TrimPrefix(strings.ToUpper(filepath.Ext(f.Path)), ".")
	if kind == "" {
		kind = "File"
	}
	if f.Symlink {
		kind = "Symbolic link"
	}
	var lines []string
	for _, field := range [][2]string{{"FILE", filepath.Base(f.Path)}, {"FOLDER", filepath.Dir(f.Path)}, {"ROOT", f.Root}, {"SIZE", ui.FormatBytes(f.Size) + " · " + kind}, {"MODIFIED", f.Modified.Format("Jan 2, 2006 15:04")}} {
		lines = append(lines, label.Render(field[0]))
		text := ansi.Hardwrap(artifactDisplay(field[1]), max(1, w), false)
		for _, line := range strings.Split(text, "\n") {
			lines = append(lines, value.Render(line))
		}
		lines = append(lines, "")
	}
	copy(out, lines)
	return out
}
