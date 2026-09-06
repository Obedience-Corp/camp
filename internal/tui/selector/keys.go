package selector

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

func neverConsumed(key tea.KeyMsg) bool {
	s := key.String()
	if s == "enter" || s == "tab" {
		return true
	}
	return strings.HasPrefix(s, "ctrl+")
}

func printableText(key tea.KeyMsg) string {
	if len(key.Runes) > 0 {
		return string(key.Runes)
	}
	return key.String()
}

func isPrintable(key tea.KeyMsg) bool {
	if key.Type == tea.KeyRunes && len(key.Runes) > 0 {
		return unicode.IsPrint(key.Runes[0])
	}
	s := key.String()
	rs := []rune(s)
	return len(rs) == 1 && unicode.IsPrint(rs[0])
}

func (m Model) updateNavigate(key tea.KeyMsg) Model {
	switch key.String() {
	case "up", "k":
		return m.move(-1)
	case "down", "j":
		return m.move(1)
	case "h", "l", "left", "right":
		return m
	case "esc", "backspace":
		return m
	case "/":
		m.mode = ModeFilter
		m.consumed = true
		return m
	}
	if isPrintable(key) {
		m.mode = ModeFilter
		m.consumed = true
		return m.appendQuery(printableText(key))
	}
	return m
}

func (m Model) updateFilter(key tea.KeyMsg) Model {
	switch key.String() {
	case "up":
		return m.move(-1)
	case "down":
		return m.move(1)
	case "left", "right":
		return m
	case "esc":
		m.consumed = true
		m.query = ""
		m.mode = ModeNavigate
		return m.rebuildKeepID()
	case "backspace":
		m.consumed = true
		return m.deleteQueryRune()
	}
	if isPrintable(key) {
		m.consumed = true
		return m.appendQuery(printableText(key))
	}
	return m
}

func (m Model) appendQuery(s string) Model {
	if s == "" {
		return m
	}
	m.query += s
	return m.rebuildKeepID()
}

func (m Model) deleteQueryRune() Model {
	if m.query == "" {
		m.mode = ModeNavigate
		return m
	}
	rs := []rune(m.query)
	m.query = string(rs[:len(rs)-1])
	if m.query == "" {
		m.mode = ModeNavigate
	}
	return m.rebuildKeepID()
}

func (m Model) move(delta int) Model {
	m.consumed = true
	n := len(m.visible)
	if n == 0 {
		return m
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next > n-1 {
		next = n - 1
	}
	m.cursor = next
	m.syncSelectedID()
	return m
}
