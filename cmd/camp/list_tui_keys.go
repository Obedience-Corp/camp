package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/ui"
)

// listKey is one browser key. The footer lists keys in table order and, as
// width runs out, drops them lowest keep first; keep 0 is never dropped.
type listKey struct {
	key     string
	label   string
	helpKey string
	help    string
	keep    int
	remotes bool // only when machines are configured
}

var listKeys = []listKey{
	{key: "g", label: "go", helpKey: "g/enter", help: "go to the selected camp", keep: 6},
	{key: "j/k", label: "move", help: "move the cursor", keep: 1},
	{key: "s", label: "status", help: "cycle status: active, inactive, reference", keep: 7},
	{key: "m", label: "move org", help: "move the selected camp to another org", keep: 5},
	{key: "o", label: "orgs", help: "list orgs and filter by one", keep: 8},
	{key: "y", label: "copy", help: "copy the selected camp's path", keep: 2},
	{key: "f", label: "filter", help: "show active camps only, or all statuses", keep: 4},
	{key: "r", label: "remotes", help: "hide or reload remote camps", keep: 3, remotes: true},
	{key: "?", label: "help", help: "show this help", keep: 9},
	{key: "q", label: "quit", helpKey: "q/esc", help: "quit", keep: 0},
}

func (m listTUIModel) shownKeys() []listKey {
	out := make([]listKey, 0, len(listKeys))
	for _, k := range listKeys {
		if k.remotes && !m.machinesConfigured {
			continue
		}
		out = append(out, k)
	}
	return out
}

// footerLevels returns the colon-style full help, then compact levels that
// each drop the lowest-keep key still shown, ending with the quit hint alone.
func (m listTUIModel) footerLevels() []string {
	keys := m.shownKeys()
	full := make([]string, 0, len(keys))
	for _, k := range keys {
		full = append(full, k.key+": "+k.label)
	}
	levels := []string{strings.Join(full, " . ")}

	minKeep := 1
	for {
		parts := make([]string, 0, len(keys))
		next := 0
		for _, k := range keys {
			if k.keep != 0 && k.keep < minKeep {
				continue
			}
			parts = append(parts, k.key+" "+k.label)
			if k.keep != 0 && (next == 0 || k.keep < next) {
				next = k.keep
			}
		}
		levels = append(levels, strings.Join(parts, " . "))
		if next == 0 {
			return levels
		}
		minKeep = next + 1
	}
}

func (m listTUIModel) footer(cw int) string {
	return listHelpStyle.Render(ui.CollapseHelp(cw, m.footerLevels()...))
}

func (m listTUIModel) updateHelp(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}
	m.overlay = listOverlayNone
	return m, nil
}

func (m listTUIModel) helpView() string {
	lay := m.layout()
	keys := m.shownKeys()
	keyW := 0
	for _, k := range keys {
		keyW = max(keyW, lipgloss.Width(k.displayHelpKey()))
	}
	title := listTitleStyle.Render("Keys")
	hint := listHelpStyle.Render("any key closes help")

	budget := m.overlayBudget(lay)
	if budget <= 0 || len(keys)+4 <= budget {
		lines := append([]string{title, ""}, helpRows(keys, keyW)...)
		return m.frame(append(lines, "", hint), lay)
	}
	// Short terminal: drop the separators, then the least important keys, but
	// keep the title and the dismiss hint.
	for len(keys) > max(budget-2, 1) {
		keys = dropLowestKeep(keys)
	}
	lines := append([]string{title}, helpRows(keys, keyW)...)
	return m.frame(append(lines, hint), lay)
}

func helpRows(keys []listKey, keyW int) []string {
	rows := make([]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, listNameStyle.Render(fmt.Sprintf("  %-*s  ", keyW, k.displayHelpKey()))+
			listMutedStyle.Render(k.help))
	}
	return rows
}

func dropLowestKeep(keys []listKey) []listKey {
	drop := -1
	for i, k := range keys {
		if k.keep != 0 && (drop < 0 || k.keep < keys[drop].keep) {
			drop = i
		}
	}
	if drop < 0 {
		return keys[:len(keys)-1]
	}
	return append(keys[:drop:drop], keys[drop+1:]...)
}

func (k listKey) displayHelpKey() string {
	if k.helpKey != "" {
		return k.helpKey
	}
	return k.key
}
