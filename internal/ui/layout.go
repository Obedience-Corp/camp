package ui

import "github.com/charmbracelet/lipgloss"

func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 3 {
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}

func ClampWidth(s string, w int) string {
	if w <= 0 {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func ClampLines(lines []string, w int) []string {
	if w <= 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ClampWidth(l, w)
	}
	return out
}

func WindowRange(cursor, total, capacity int) (int, int) {
	if capacity >= total {
		return 0, total
	}
	start := cursor - capacity/2
	if start < 0 {
		start = 0
	}
	end := start + capacity
	if end > total {
		end = total
		start = end - capacity
	}
	return max(start, 0), end
}

func CursorGlyph(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}

func ClampIdx(v, n int) int {
	if n <= 0 {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > n-1 {
		return n - 1
	}
	return v
}
