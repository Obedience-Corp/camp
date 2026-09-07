//go:build integration
// +build integration

package integration

import (
	"strings"
	"unicode/utf8"
)

const (
	ttyAltEnter = "\x1b[?1049h"
	ttyAltLeave = "\x1b[?1049l"
)

// ttyVisibleLines reconstructs the last rendered screen from a TTY transcript.
// TUIs repaint by homing and overwriting; the full byte stream is not the
// layout a user sees. This keeps CUP/ED/EL/CR/LF and ignores styling.
// ttyCUPRows maps 1-based terminal rows to the last text tcell wrote there
// with CUP (`ESC [ row ; col H`). go-fuzzyfinder addresses the prompt by row,
// so this is the geometry a user sees, not the overwrite-collapsed stream.
func ttyCUPRows(raw string) map[int]string {
	const width = 160
	cells := make(map[int][]rune)
	rowAt := func(row int) []rune {
		buf := cells[row]
		if buf == nil {
			buf = make([]rune, width)
			for i := range buf {
				buf[i] = ' '
			}
			cells[row] = buf
		}
		return buf
	}
	i := 0
	for i < len(raw) {
		if raw[i] != '\x1b' {
			i++
			continue
		}
		next, cmd, args, ok := parseCSI(raw, i)
		if !ok {
			i++
			continue
		}
		if cmd != 'H' && cmd != 'f' {
			i = next
			continue
		}
		row, col := 1, 1
		if len(args) > 0 && args[0] > 0 {
			row = args[0]
		}
		if len(args) > 1 && args[1] > 0 {
			col = args[1]
		}
		buf := rowAt(row)
		c := col - 1
		j := next
		for j < len(raw) {
			if raw[j] == '\x1b' {
				n2, cmd2, _, ok := parseCSI(raw, j)
				if !ok || cmd2 == 'H' || cmd2 == 'f' {
					break
				}
				j = n2
				continue
			}
			if raw[j] == '\r' || raw[j] == '\n' {
				break
			}
			ch, size := utf8.DecodeRuneInString(raw[j:])
			if ch == utf8.RuneError && size == 1 {
				j++
				continue
			}
			if c >= 0 && c < width {
				buf[c] = ch
			}
			c++
			j += size
		}
		i = j
	}
	rows := make(map[int]string, len(cells))
	for row, buf := range cells {
		if text := strings.TrimSpace(string(buf)); text != "" {
			rows[row] = text
		}
	}
	return rows
}

// ttyFreezeAtPrompt keeps the last clear-to-clear paint that contains both the
// prompt and the recency winner, dropping alt-screen teardown after it.
func ttyFreezeAtPrompt(raw, prompt, winner string) string {
	if i := strings.Index(raw, ttyAltLeave); i >= 0 {
		raw = raw[:i]
	}
	parts := strings.Split(raw, "\x1b[2J")
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.Contains(parts[i], prompt) && strings.Contains(parts[i], winner) {
			return parts[i]
		}
	}
	return raw
}

func ttyVisibleLines(raw string, rows, cols int) []string {
	if rows <= 0 || cols <= 0 {
		return nil
	}
	if i := strings.LastIndex(raw, ttyAltEnter); i >= 0 {
		raw = raw[i+len(ttyAltEnter):]
	}
	if i := strings.Index(raw, ttyAltLeave); i >= 0 {
		raw = raw[:i]
	}

	grid := make([][]rune, rows)
	for r := range grid {
		grid[r] = make([]rune, cols)
		for c := range grid[r] {
			grid[r][c] = ' '
		}
	}
	row, col := 0, 0
	put := func(ch rune) {
		if row < 0 || row >= rows || col < 0 || col >= cols {
			return
		}
		grid[row][col] = ch
		col++
	}
	eraseLine := func(mode int) {
		if row < 0 || row >= rows {
			return
		}
		switch mode {
		case 1:
			for c := 0; c <= col && c < cols; c++ {
				grid[row][c] = ' '
			}
		case 2:
			for c := 0; c < cols; c++ {
				grid[row][c] = ' '
			}
		default:
			for c := col; c < cols; c++ {
				grid[row][c] = ' '
			}
		}
	}
	eraseDisplay := func(mode int) {
		switch mode {
		case 1:
			for r := 0; r < row && r < rows; r++ {
				for c := 0; c < cols; c++ {
					grid[r][c] = ' '
				}
			}
			eraseLine(1)
		case 2, 3:
			for r := 0; r < rows; r++ {
				for c := 0; c < cols; c++ {
					grid[r][c] = ' '
				}
			}
			row, col = 0, 0
		default:
			eraseLine(0)
			for r := row + 1; r < rows; r++ {
				for c := 0; c < cols; c++ {
					grid[r][c] = ' '
				}
			}
		}
	}

	i := 0
	for i < len(raw) {
		if raw[i] == '\x1b' {
			next, cmd, args, ok := parseCSI(raw, i)
			if !ok {
				i++
				continue
			}
			i = next
			arg := func(n, fallback int) int {
				if n < len(args) && args[n] != 0 {
					return args[n]
				}
				return fallback
			}
			switch cmd {
			case 'H', 'f':
				row = arg(0, 1) - 1
				col = arg(1, 1) - 1
			case 'A':
				row -= arg(0, 1)
			case 'B':
				row += arg(0, 1)
			case 'C':
				col += arg(0, 1)
			case 'D':
				col -= arg(0, 1)
			case 'G':
				col = arg(0, 1) - 1
			case 'J':
				eraseDisplay(arg(0, 0))
			case 'K':
				eraseLine(arg(0, 0))
			}
			if row < 0 {
				row = 0
			}
			if col < 0 {
				col = 0
			}
			continue
		}
		switch raw[i] {
		case '\n':
			row++
			col = 0
			i++
		case '\r':
			col = 0
			i++
		case '\b':
			if col > 0 {
				col--
			}
			i++
		case '\t':
			col = (col + 8) / 8 * 8
			i++
		default:
			ch, size := utf8.DecodeRuneInString(raw[i:])
			if ch == utf8.RuneError && size == 1 {
				i++
				continue
			}
			put(ch)
			i += size
		}
	}

	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		line := strings.TrimRight(string(grid[r]), " ")
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func parseCSI(raw string, i int) (next int, cmd byte, args []int, ok bool) {
	if i+1 >= len(raw) || raw[i] != '\x1b' {
		return i, 0, nil, false
	}
	if raw[i+1] != '[' {
		// OSC and other ESC sequences: skip until ST/BEL or one extra byte.
		if raw[i+1] == ']' {
			j := i + 2
			for j < len(raw) {
				if raw[j] == '\a' {
					return j + 1, 0, nil, true
				}
				if raw[j] == '\x1b' && j+1 < len(raw) && raw[j+1] == '\\' {
					return j + 2, 0, nil, true
				}
				j++
			}
			return len(raw), 0, nil, true
		}
		if i+2 < len(raw) && (raw[i+1] == '(' || raw[i+1] == ')' || raw[i+1] == '*' || raw[i+1] == '+') {
			return i + 3, 0, nil, true
		}
		return i + 2, 0, nil, true
	}
	j := i + 2
	if j < len(raw) && (raw[j] == '?' || raw[j] == '>') {
		j++
	}
	n := 0
	saw := false
	for j < len(raw) {
		ch := raw[j]
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
			saw = true
			j++
			continue
		}
		if ch == ';' {
			args = append(args, n)
			n = 0
			saw = false
			j++
			continue
		}
		if ch >= '@' && ch <= '~' {
			if saw || len(args) > 0 {
				args = append(args, n)
			}
			return j + 1, ch, args, true
		}
		j++
	}
	return len(raw), 0, nil, true
}
