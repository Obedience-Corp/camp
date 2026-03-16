package vim

import (
	"unicode"
)

// Motion represents the result of a motion command.
type Motion struct {
	Start    Position
	End      Position
	Linewise bool // True for line-based motions (dd, yy, etc.)
}

// MoveLeft moves cursor left by count characters.
func MoveLeft(b *Buffer, count int) Motion {
	start := b.Cursor()
	col := start.Col - count
	if col < 0 {
		col = 0
	}
	b.SetCursor(Position{Line: start.Line, Col: col})
	return Motion{Start: start, End: b.Cursor()}
}

// MoveRight moves cursor right by count characters.
func MoveRight(b *Buffer, count int) Motion {
	start := b.Cursor()
	col := start.Col + count
	lineLen := b.CurrentLineLen()
	if col >= lineLen && lineLen > 0 {
		col = lineLen - 1
	}
	if col < 0 {
		col = 0
	}
	b.SetCursor(Position{Line: start.Line, Col: col})
	return Motion{Start: start, End: b.Cursor()}
}

// MoveDown moves cursor down by count lines.
func MoveDown(b *Buffer, count int) Motion {
	start := b.Cursor()
	line := start.Line + count
	if line >= b.LineCount() {
		line = b.LineCount() - 1
	}
	if line < 0 {
		line = 0
	}
	b.SetCursor(Position{Line: line, Col: start.Col})
	return Motion{Start: start, End: b.Cursor(), Linewise: true}
}

// MoveUp moves cursor up by count lines.
func MoveUp(b *Buffer, count int) Motion {
	start := b.Cursor()
	line := start.Line - count
	if line < 0 {
		line = 0
	}
	b.SetCursor(Position{Line: line, Col: start.Col})
	return Motion{Start: start, End: b.Cursor(), Linewise: true}
}

// MoveToLineStart moves cursor to start of line (column 0).
func MoveToLineStart(b *Buffer) Motion {
	start := b.Cursor()
	b.SetCursor(Position{Line: start.Line, Col: 0})
	return Motion{Start: start, End: b.Cursor()}
}

// MoveToLineEnd moves cursor to end of line.
func MoveToLineEnd(b *Buffer) Motion {
	start := b.Cursor()
	lineLen := b.CurrentLineLen()
	col := lineLen - 1
	if col < 0 {
		col = 0
	}
	b.SetCursor(Position{Line: start.Line, Col: col})
	return Motion{Start: start, End: b.Cursor()}
}

// MoveToFirstNonBlank moves cursor to first non-blank character.
func MoveToFirstNonBlank(b *Buffer) Motion {
	start := b.Cursor()
	b.FirstNonBlank()
	return Motion{Start: start, End: b.Cursor()}
}

// MoveWordForward moves cursor forward by count words.
func MoveWordForward(b *Buffer, count int) Motion {
	start := b.Cursor()
	content := b.Content()
	offset := b.CursorOffset()

	for i := 0; i < count && offset < len(content); i++ {
		offset = nextWordStart(content, offset)
	}

	b.SetCursorFromOffset(offset)
	return Motion{Start: start, End: b.Cursor()}
}

// MoveWordBackward moves cursor backward by count words.
func MoveWordBackward(b *Buffer, count int) Motion {
	start := b.Cursor()
	content := b.Content()
	offset := b.CursorOffset()

	for i := 0; i < count && offset > 0; i++ {
		offset = prevWordStart(content, offset)
	}

	b.SetCursorFromOffset(offset)
	return Motion{Start: start, End: b.Cursor()}
}

// MoveWordEnd moves cursor to end of current/next word.
func MoveWordEnd(b *Buffer, count int) Motion {
	start := b.Cursor()
	content := b.Content()
	offset := b.CursorOffset()

	for i := 0; i < count && offset < len(content)-1; i++ {
		offset = wordEnd(content, offset)
	}

	b.SetCursorFromOffset(offset)
	return Motion{Start: start, End: b.Cursor()}
}

// MoveBigWordForward moves cursor forward by count WORDs (whitespace-delimited).
func MoveBigWordForward(b *Buffer, count int) Motion {
	start := b.Cursor()
	content := b.Content()
	offset := b.CursorOffset()

	for i := 0; i < count && offset < len(content); i++ {
		offset = nextBigWordStart(content, offset)
	}

	b.SetCursorFromOffset(offset)
	return Motion{Start: start, End: b.Cursor()}
}

// MoveBigWordBackward moves cursor backward by count WORDs.
func MoveBigWordBackward(b *Buffer, count int) Motion {
	start := b.Cursor()
	content := b.Content()
	offset := b.CursorOffset()

	for i := 0; i < count && offset > 0; i++ {
		offset = prevBigWordStart(content, offset)
	}

	b.SetCursorFromOffset(offset)
	return Motion{Start: start, End: b.Cursor()}
}

// MoveBigWordEnd moves cursor to end of current/next WORD.
func MoveBigWordEnd(b *Buffer, count int) Motion {
	start := b.Cursor()
	content := b.Content()
	offset := b.CursorOffset()

	for i := 0; i < count && offset < len(content)-1; i++ {
		offset = bigWordEnd(content, offset)
	}

	b.SetCursorFromOffset(offset)
	return Motion{Start: start, End: b.Cursor()}
}

// MoveToDocumentStart moves cursor to start of document.
func MoveToDocumentStart(b *Buffer) Motion {
	start := b.Cursor()
	b.SetCursor(Position{Line: 0, Col: 0})
	b.FirstNonBlank()
	return Motion{Start: start, End: b.Cursor(), Linewise: true}
}

// MoveToDocumentEnd moves cursor to last line of document.
func MoveToDocumentEnd(b *Buffer) Motion {
	start := b.Cursor()
	b.SetCursor(Position{Line: b.LineCount() - 1, Col: 0})
	b.FirstNonBlank()
	return Motion{Start: start, End: b.Cursor(), Linewise: true}
}

// MoveToLine moves cursor to a specific line number (1-indexed).
func MoveToLine(b *Buffer, lineNum int) Motion {
	start := b.Cursor()
	line := lineNum - 1 // Convert to 0-indexed
	if line < 0 {
		line = 0
	}
	if line >= b.LineCount() {
		line = b.LineCount() - 1
	}
	b.SetCursor(Position{Line: line, Col: 0})
	b.FirstNonBlank()
	return Motion{Start: start, End: b.Cursor(), Linewise: true}
}

// FindCharForward finds the count-th occurrence of char forward.
func FindCharForward(b *Buffer, char rune, count int, till bool) Motion {
	start := b.Cursor()
	line := b.CurrentLine()

	found := 0
	for i := start.Col + 1; i < len(line); i++ {
		if rune(line[i]) == char {
			found++
			if found == count {
				col := i
				if till {
					col-- // Stop before the character
				}
				b.SetCursor(Position{Line: start.Line, Col: col})
				return Motion{Start: start, End: b.Cursor()}
			}
		}
	}

	// Not found, don't move
	return Motion{Start: start, End: start}
}

// FindCharBackward finds the count-th occurrence of char backward.
func FindCharBackward(b *Buffer, char rune, count int, till bool) Motion {
	start := b.Cursor()
	line := b.CurrentLine()

	found := 0
	for i := start.Col - 1; i >= 0; i-- {
		if rune(line[i]) == char {
			found++
			if found == count {
				col := i
				if till {
					col++ // Stop after the character
				}
				b.SetCursor(Position{Line: start.Line, Col: col})
				return Motion{Start: start, End: b.Cursor()}
			}
		}
	}

	// Not found, don't move
	return Motion{Start: start, End: start}
}

// MoveParagraphForward moves to start of next paragraph.
func MoveParagraphForward(b *Buffer, count int) Motion {
	start := b.Cursor()

	for i := 0; i < count; i++ {
		line := b.Cursor().Line
		// Skip current non-empty lines
		for line < b.LineCount()-1 && len(b.Lines()[line]) > 0 {
			line++
		}
		// Skip empty lines
		for line < b.LineCount()-1 && len(b.Lines()[line]) == 0 {
			line++
		}
		b.SetCursor(Position{Line: line, Col: 0})
	}

	return Motion{Start: start, End: b.Cursor(), Linewise: true}
}

// MoveParagraphBackward moves to start of previous paragraph.
func MoveParagraphBackward(b *Buffer, count int) Motion {
	start := b.Cursor()

	for i := 0; i < count; i++ {
		line := b.Cursor().Line
		// Skip current empty lines
		for line > 0 && len(b.Lines()[line]) == 0 {
			line--
		}
		// Skip non-empty lines
		for line > 0 && len(b.Lines()[line]) > 0 {
			line--
		}
		b.SetCursor(Position{Line: line, Col: 0})
	}

	return Motion{Start: start, End: b.Cursor(), Linewise: true}
}

// Helper functions for word navigation

func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func nextWordStart(content string, offset int) int {
	n := len(content)
	if offset >= n {
		return n
	}

	// Skip current word
	if offset < n && isWordChar(rune(content[offset])) {
		for offset < n && isWordChar(rune(content[offset])) {
			offset++
		}
	} else if offset < n && !unicode.IsSpace(rune(content[offset])) {
		// Skip punctuation
		for offset < n && !isWordChar(rune(content[offset])) && !unicode.IsSpace(rune(content[offset])) {
			offset++
		}
	}

	// Skip whitespace
	for offset < n && unicode.IsSpace(rune(content[offset])) {
		offset++
	}

	return offset
}

func prevWordStart(content string, offset int) int {
	if offset <= 0 {
		return 0
	}
	offset--

	// Skip whitespace backward
	for offset > 0 && unicode.IsSpace(rune(content[offset])) {
		offset--
	}

	// Find start of word
	if offset >= 0 && isWordChar(rune(content[offset])) {
		for offset > 0 && isWordChar(rune(content[offset-1])) {
			offset--
		}
	} else if offset >= 0 {
		// Punctuation
		for offset > 0 && !isWordChar(rune(content[offset-1])) && !unicode.IsSpace(rune(content[offset-1])) {
			offset--
		}
	}

	return offset
}

func wordEnd(content string, offset int) int {
	n := len(content)
	if offset >= n-1 {
		return n - 1
	}
	offset++

	// Skip whitespace
	for offset < n && unicode.IsSpace(rune(content[offset])) {
		offset++
	}

	// Move to end of word
	if offset < n && isWordChar(rune(content[offset])) {
		for offset < n-1 && isWordChar(rune(content[offset+1])) {
			offset++
		}
	} else {
		// Punctuation
		for offset < n-1 && !isWordChar(rune(content[offset+1])) && !unicode.IsSpace(rune(content[offset+1])) {
			offset++
		}
	}

	return offset
}

func nextBigWordStart(content string, offset int) int {
	n := len(content)
	if offset >= n {
		return n
	}

	// Skip current WORD (non-whitespace)
	for offset < n && !unicode.IsSpace(rune(content[offset])) {
		offset++
	}

	// Skip whitespace
	for offset < n && unicode.IsSpace(rune(content[offset])) {
		offset++
	}

	return offset
}

func prevBigWordStart(content string, offset int) int {
	if offset <= 0 {
		return 0
	}
	offset--

	// Skip whitespace backward
	for offset > 0 && unicode.IsSpace(rune(content[offset])) {
		offset--
	}

	// Find start of WORD
	for offset > 0 && !unicode.IsSpace(rune(content[offset-1])) {
		offset--
	}

	return offset
}

func bigWordEnd(content string, offset int) int {
	n := len(content)
	if offset >= n-1 {
		return n - 1
	}
	offset++

	// Skip whitespace
	for offset < n && unicode.IsSpace(rune(content[offset])) {
		offset++
	}

	// Move to end of WORD
	for offset < n-1 && !unicode.IsSpace(rune(content[offset+1])) {
		offset++
	}

	return offset
}

// bracketPairs maps each bracket to its matching counterpart and direction.
var bracketPairs = map[rune]rune{
	'(': ')', ')': '(',
	'{': '}', '}': '{',
	'[': ']', ']': '[',
}

// isOpenBracket returns true for opening brackets.
func isOpenBracket(r rune) bool {
	return r == '(' || r == '{' || r == '['
}

// isBracket returns true for any bracket character.
func isBracket(r rune) bool {
	_, ok := bracketPairs[r]
	return ok
}

// MatchBracket implements vim's % motion: jump to matching bracket.
func MatchBracket(b *Buffer) Motion {
	start := b.Cursor()
	content := b.Content()
	offset := b.CursorOffset()

	ch := b.CharUnderCursor()

	// If not on a bracket, scan forward on current line to find one.
	if !isBracket(ch) {
		line := b.CurrentLine()
		found := false
		for i := start.Col; i < len(line); i++ {
			if isBracket(rune(line[i])) {
				// Move cursor to this bracket, then match from there.
				offset = offset + (i - start.Col)
				ch = rune(line[i])
				found = true
				break
			}
		}
		if !found {
			return Motion{Start: start, End: start}
		}
	}

	var matchOffset int
	if isOpenBracket(ch) {
		matchOffset = findMatchForward(content, offset, ch, bracketPairs[ch])
	} else {
		matchOffset = findMatchBackward(content, offset, bracketPairs[ch], ch)
	}

	if matchOffset == -1 {
		return Motion{Start: start, End: start}
	}

	b.SetCursorFromOffset(matchOffset)
	return Motion{Start: start, End: b.Cursor()}
}

// findMatchForward finds the matching close bracket scanning forward.
// Cursor must be on the open bracket at offset.
func findMatchForward(content string, offset int, open, close rune) int {
	depth := 0
	for i := offset; i < len(content); i++ {
		ch := rune(content[i])
		if ch == open {
			depth++
		} else if ch == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// findMatchBackward finds the matching open bracket scanning backward.
// Cursor must be on the close bracket at offset.
func findMatchBackward(content string, offset int, open, close rune) int {
	depth := 0
	for i := offset; i >= 0; i-- {
		ch := rune(content[i])
		if ch == close {
			depth++
		} else if ch == open {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
