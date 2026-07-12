package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

// FitFullscreenView keeps a Bubble Tea full-screen view within the terminal's
// row budget. Bubble Tea splits views on newlines and, when there are too many
// rows, keeps the bottom rows. A trailing newline therefore creates a phantom
// row that can evict the title or top border. Keep the top of the view instead.
func FitFullscreenView(view string, height int) string {
	view = strings.TrimRight(view, "\n")
	if height <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
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
