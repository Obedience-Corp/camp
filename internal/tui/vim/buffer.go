package vim

import (
	"strings"
	"unicode"
)

// Buffer provides vim-style operations on a text buffer.
type Buffer struct {
	lines  []string
	cursor Position
	yank   string // Yanked text (clipboard)
}

// Position represents a cursor position in the buffer.
type Position struct {
	Line int
	Col  int
}

// NewBuffer creates a new buffer from text content.
func NewBuffer(content string) *Buffer {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &Buffer{
		lines:  lines,
		cursor: Position{Line: 0, Col: 0},
	}
}

// Content returns the buffer content as a single string.
func (b *Buffer) Content() string {
	return strings.Join(b.lines, "\n")
}

// Lines returns the buffer lines.
func (b *Buffer) Lines() []string {
	return b.lines
}

// Cursor returns the current cursor position.
func (b *Buffer) Cursor() Position {
	return b.cursor
}

// SetCursor sets the cursor position with bounds checking.
// In normal mode, cursor is clamped to lineLen-1 (on last char).
func (b *Buffer) SetCursor(pos Position) {
	b.setCursor(pos, false)
}

// SetCursorInsert sets cursor position for insert mode (allows col == lineLen).
func (b *Buffer) SetCursorInsert(pos Position) {
	b.setCursor(pos, true)
}

// setCursor is the internal cursor setter.
func (b *Buffer) setCursor(pos Position, insertMode bool) {
	if pos.Line < 0 {
		pos.Line = 0
	}
	if pos.Line >= len(b.lines) {
		pos.Line = len(b.lines) - 1
	}
	if pos.Line < 0 {
		pos.Line = 0
	}

	lineLen := len(b.lines[pos.Line])
	if pos.Col < 0 {
		pos.Col = 0
	}

	if insertMode {
		// In insert mode, cursor can be at lineLen (for appending)
		if pos.Col > lineLen {
			pos.Col = lineLen
		}
	} else {
		// In normal mode, cursor is on last char
		if pos.Col >= lineLen && lineLen > 0 {
			pos.Col = lineLen - 1
		}
	}
	if pos.Col < 0 {
		pos.Col = 0
	}

	b.cursor = pos
}

// CursorOffset returns the absolute offset of the cursor in the content.
func (b *Buffer) CursorOffset() int {
	offset := 0
	for i := 0; i < b.cursor.Line; i++ {
		offset += len(b.lines[i]) + 1 // +1 for newline
	}
	offset += b.cursor.Col
	return offset
}

// SetCursorFromOffset sets cursor position from absolute offset.
func (b *Buffer) SetCursorFromOffset(offset int) {
	currentOffset := 0
	for i, line := range b.lines {
		lineEnd := currentOffset + len(line)
		if offset <= lineEnd || i == len(b.lines)-1 {
			b.cursor.Line = i
			b.cursor.Col = offset - currentOffset
			if b.cursor.Col > len(line) {
				b.cursor.Col = len(line)
			}
			if b.cursor.Col < 0 {
				b.cursor.Col = 0
			}
			return
		}
		currentOffset = lineEnd + 1 // +1 for newline
	}
}

// CurrentLine returns the current line.
func (b *Buffer) CurrentLine() string {
	if b.cursor.Line >= 0 && b.cursor.Line < len(b.lines) {
		return b.lines[b.cursor.Line]
	}
	return ""
}

// CurrentLineLen returns the length of the current line.
func (b *Buffer) CurrentLineLen() int {
	return len(b.CurrentLine())
}

// CharUnderCursor returns the character under the cursor.
func (b *Buffer) CharUnderCursor() rune {
	line := b.CurrentLine()
	if b.cursor.Col >= 0 && b.cursor.Col < len(line) {
		return rune(line[b.cursor.Col])
	}
	return 0
}

// LineCount returns the number of lines.
func (b *Buffer) LineCount() int {
	return len(b.lines)
}

// Insert inserts text at the current cursor position.
func (b *Buffer) Insert(text string) {
	if len(b.lines) == 0 {
		b.lines = []string{""}
	}

	line := b.lines[b.cursor.Line]
	col := b.cursor.Col
	if col > len(line) {
		col = len(line)
	}

	before := line[:col]
	after := line[col:]

	// Handle multi-line insert
	insertLines := strings.Split(text, "\n")
	if len(insertLines) == 1 {
		b.lines[b.cursor.Line] = before + text + after
		b.cursor.Col += len(text)
	} else {
		// Multi-line insert
		firstLine := before + insertLines[0]
		lastLine := insertLines[len(insertLines)-1] + after

		newLines := make([]string, 0, len(b.lines)+len(insertLines)-1)
		newLines = append(newLines, b.lines[:b.cursor.Line]...)
		newLines = append(newLines, firstLine)
		newLines = append(newLines, insertLines[1:len(insertLines)-1]...)
		newLines = append(newLines, lastLine)
		newLines = append(newLines, b.lines[b.cursor.Line+1:]...)

		b.lines = newLines
		b.cursor.Line += len(insertLines) - 1
		b.cursor.Col = len(insertLines[len(insertLines)-1])
	}
}

// DeleteChar deletes the character under the cursor.
func (b *Buffer) DeleteChar() string {
	line := b.CurrentLine()
	if b.cursor.Col >= len(line) {
		// At end of line, join with next line
		if b.cursor.Line < len(b.lines)-1 {
			b.lines[b.cursor.Line] = line + b.lines[b.cursor.Line+1]
			b.lines = append(b.lines[:b.cursor.Line+1], b.lines[b.cursor.Line+2:]...)
			return "\n"
		}
		return ""
	}

	deleted := string(line[b.cursor.Col])
	b.lines[b.cursor.Line] = line[:b.cursor.Col] + line[b.cursor.Col+1:]
	return deleted
}

// DeleteCharBefore deletes the character before the cursor (backspace).
func (b *Buffer) DeleteCharBefore() string {
	if b.cursor.Col > 0 {
		line := b.CurrentLine()
		// Bounds check - clamp cursor if beyond line content
		if b.cursor.Col > len(line) {
			b.cursor.Col = len(line)
		}
		// Check again after clamping
		if b.cursor.Col > 0 {
			deleted := string(line[b.cursor.Col-1])
			b.lines[b.cursor.Line] = line[:b.cursor.Col-1] + line[b.cursor.Col:]
			b.cursor.Col--
			return deleted
		}
	}

	// At start of line, join with previous line
	if b.cursor.Line > 0 {
		prevLine := b.lines[b.cursor.Line-1]
		b.lines[b.cursor.Line-1] = prevLine + b.CurrentLine()
		b.lines = append(b.lines[:b.cursor.Line], b.lines[b.cursor.Line+1:]...)
		b.cursor.Line--
		b.cursor.Col = len(prevLine)
		return "\n"
	}

	return ""
}

// DeleteLine deletes the current line.
func (b *Buffer) DeleteLine() string {
	if len(b.lines) == 0 {
		return ""
	}

	deleted := b.lines[b.cursor.Line] + "\n"

	if len(b.lines) == 1 {
		b.lines = []string{""}
		b.cursor.Col = 0
	} else {
		b.lines = append(b.lines[:b.cursor.Line], b.lines[b.cursor.Line+1:]...)
		if b.cursor.Line >= len(b.lines) {
			b.cursor.Line = len(b.lines) - 1
		}
	}

	// Move to first non-blank character
	b.cursor.Col = b.firstNonBlank(b.cursor.Line)
	return deleted
}

// DeleteToEndOfLine deletes from cursor to end of line.
func (b *Buffer) DeleteToEndOfLine() string {
	line := b.CurrentLine()
	if b.cursor.Col >= len(line) {
		return ""
	}
	deleted := line[b.cursor.Col:]
	b.lines[b.cursor.Line] = line[:b.cursor.Col]
	if b.cursor.Col > 0 {
		b.cursor.Col--
	}
	return deleted
}

// DeleteRange deletes text between two positions.
func (b *Buffer) DeleteRange(start, end Position) string {
	// Ensure start <= end
	if start.Line > end.Line || (start.Line == end.Line && start.Col > end.Col) {
		start, end = end, start
	}

	if start.Line == end.Line {
		// Same line
		line := b.lines[start.Line]
		endCol := end.Col + 1
		if endCol > len(line) {
			endCol = len(line)
		}
		deleted := line[start.Col:endCol]
		b.lines[start.Line] = line[:start.Col] + line[endCol:]
		b.cursor = start
		return deleted
	}

	// Multi-line delete
	var deleted strings.Builder

	// First line portion
	firstLine := b.lines[start.Line]
	deleted.WriteString(firstLine[start.Col:])
	deleted.WriteString("\n")

	// Middle lines
	for i := start.Line + 1; i < end.Line; i++ {
		deleted.WriteString(b.lines[i])
		deleted.WriteString("\n")
	}

	// Last line portion
	lastLine := b.lines[end.Line]
	endCol := end.Col + 1
	if endCol > len(lastLine) {
		endCol = len(lastLine)
	}
	deleted.WriteString(lastLine[:endCol])

	// Reconstruct
	newLine := firstLine[:start.Col] + lastLine[endCol:]
	newLines := make([]string, 0, len(b.lines)-(end.Line-start.Line))
	newLines = append(newLines, b.lines[:start.Line]...)
	newLines = append(newLines, newLine)
	newLines = append(newLines, b.lines[end.Line+1:]...)
	b.lines = newLines

	b.cursor = start
	return deleted.String()
}

// YankRange copies text between two positions to the yank register.
func (b *Buffer) YankRange(start, end Position) string {
	// Ensure start <= end
	if start.Line > end.Line || (start.Line == end.Line && start.Col > end.Col) {
		start, end = end, start
	}

	if start.Line == end.Line {
		line := b.lines[start.Line]
		endCol := end.Col + 1
		if endCol > len(line) {
			endCol = len(line)
		}
		b.yank = line[start.Col:endCol]
		return b.yank
	}

	// Multi-line yank
	var yanked strings.Builder
	firstLine := b.lines[start.Line]
	yanked.WriteString(firstLine[start.Col:])
	yanked.WriteString("\n")

	for i := start.Line + 1; i < end.Line; i++ {
		yanked.WriteString(b.lines[i])
		yanked.WriteString("\n")
	}

	lastLine := b.lines[end.Line]
	endCol := end.Col + 1
	if endCol > len(lastLine) {
		endCol = len(lastLine)
	}
	yanked.WriteString(lastLine[:endCol])

	b.yank = yanked.String()
	return b.yank
}

// YankLine copies the current line to the yank register.
func (b *Buffer) YankLine() string {
	b.yank = b.CurrentLine() + "\n"
	return b.yank
}

// Paste inserts yanked text after the cursor.
func (b *Buffer) Paste() {
	if b.yank == "" {
		return
	}

	// If yank ends with newline, paste on next line
	if strings.HasSuffix(b.yank, "\n") {
		content := strings.TrimSuffix(b.yank, "\n")
		newLines := strings.Split(content, "\n")

		insertAt := b.cursor.Line + 1
		combined := make([]string, 0, len(b.lines)+len(newLines))
		combined = append(combined, b.lines[:insertAt]...)
		combined = append(combined, newLines...)
		combined = append(combined, b.lines[insertAt:]...)
		b.lines = combined

		b.cursor.Line = insertAt
		b.cursor.Col = b.firstNonBlank(insertAt)
	} else {
		// Paste inline after cursor
		b.cursor.Col++
		if b.cursor.Col > len(b.CurrentLine()) {
			b.cursor.Col = len(b.CurrentLine())
		}
		b.Insert(b.yank)
	}
}

// PasteBefore inserts yanked text before the cursor.
func (b *Buffer) PasteBefore() {
	if b.yank == "" {
		return
	}

	if strings.HasSuffix(b.yank, "\n") {
		content := strings.TrimSuffix(b.yank, "\n")
		newLines := strings.Split(content, "\n")

		insertAt := b.cursor.Line
		combined := make([]string, 0, len(b.lines)+len(newLines))
		combined = append(combined, b.lines[:insertAt]...)
		combined = append(combined, newLines...)
		combined = append(combined, b.lines[insertAt:]...)
		b.lines = combined

		b.cursor.Col = b.firstNonBlank(insertAt)
	} else {
		b.Insert(b.yank)
	}
}

// ReplaceChar replaces the character under cursor.
func (b *Buffer) ReplaceChar(r rune) {
	line := b.CurrentLine()
	if b.cursor.Col < len(line) {
		b.lines[b.cursor.Line] = line[:b.cursor.Col] + string(r) + line[b.cursor.Col+1:]
	}
}

// JoinLines joins the current line with the next.
func (b *Buffer) JoinLines() {
	if b.cursor.Line >= len(b.lines)-1 {
		return
	}

	currentLine := b.lines[b.cursor.Line]
	nextLine := strings.TrimLeft(b.lines[b.cursor.Line+1], " \t")

	joinCol := len(currentLine)
	if joinCol > 0 && !strings.HasSuffix(currentLine, " ") {
		b.lines[b.cursor.Line] = currentLine + " " + nextLine
		joinCol++
	} else {
		b.lines[b.cursor.Line] = currentLine + nextLine
	}

	b.lines = append(b.lines[:b.cursor.Line+1], b.lines[b.cursor.Line+2:]...)
	b.cursor.Col = joinCol
}

// NewLine inserts a new line below and moves cursor.
func (b *Buffer) NewLineBelow() {
	newLines := make([]string, 0, len(b.lines)+1)
	newLines = append(newLines, b.lines[:b.cursor.Line+1]...)
	newLines = append(newLines, "")
	newLines = append(newLines, b.lines[b.cursor.Line+1:]...)
	b.lines = newLines
	b.cursor.Line++
	b.cursor.Col = 0
}

// NewLineAbove inserts a new line above and moves cursor.
func (b *Buffer) NewLineAbove() {
	newLines := make([]string, 0, len(b.lines)+1)
	newLines = append(newLines, b.lines[:b.cursor.Line]...)
	newLines = append(newLines, "")
	newLines = append(newLines, b.lines[b.cursor.Line:]...)
	b.lines = newLines
	b.cursor.Col = 0
}

// firstNonBlank returns the column of the first non-blank character on a line.
func (b *Buffer) firstNonBlank(line int) int {
	if line < 0 || line >= len(b.lines) {
		return 0
	}
	for i, r := range b.lines[line] {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return 0
}

// FirstNonBlank moves cursor to first non-blank character on current line.
func (b *Buffer) FirstNonBlank() {
	b.cursor.Col = b.firstNonBlank(b.cursor.Line)
}

// SetContent replaces the entire buffer content.
func (b *Buffer) SetContent(content string) {
	b.lines = strings.Split(content, "\n")
	if len(b.lines) == 0 {
		b.lines = []string{""}
	}
	// Ensure cursor is valid
	if b.cursor.Line >= len(b.lines) {
		b.cursor.Line = len(b.lines) - 1
	}
	if b.cursor.Col >= len(b.lines[b.cursor.Line]) {
		b.cursor.Col = 0
		if len(b.lines[b.cursor.Line]) > 0 {
			b.cursor.Col = len(b.lines[b.cursor.Line]) - 1
		}
	}
}

// Yank returns the current yank register content.
func (b *Buffer) Yank() string {
	return b.yank
}

// SetYank sets the yank register content.
func (b *Buffer) SetYank(content string) {
	b.yank = content
}
