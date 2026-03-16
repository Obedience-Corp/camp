package vim

import (
	"unicode"
)

// TextObject represents a text selection from a text object.
type TextObject struct {
	Start Position
	End   Position
	Found bool
}

// InnerWord selects the word under cursor (iw).
func InnerWord(b *Buffer) TextObject {
	line := b.CurrentLine()
	col := b.Cursor().Col

	if col >= len(line) {
		return TextObject{Found: false}
	}

	// Check if on a word character
	if !isWordChar(rune(line[col])) {
		// Select whitespace or punctuation
		start := col
		end := col

		if unicode.IsSpace(rune(line[col])) {
			// Select whitespace
			for start > 0 && unicode.IsSpace(rune(line[start-1])) {
				start--
			}
			for end < len(line)-1 && unicode.IsSpace(rune(line[end+1])) {
				end++
			}
		} else {
			// Select punctuation
			for start > 0 && !isWordChar(rune(line[start-1])) && !unicode.IsSpace(rune(line[start-1])) {
				start--
			}
			for end < len(line)-1 && !isWordChar(rune(line[end+1])) && !unicode.IsSpace(rune(line[end+1])) {
				end++
			}
		}

		return TextObject{
			Start: Position{Line: b.Cursor().Line, Col: start},
			End:   Position{Line: b.Cursor().Line, Col: end},
			Found: true,
		}
	}

	// Find word boundaries
	start := col
	end := col

	for start > 0 && isWordChar(rune(line[start-1])) {
		start--
	}
	for end < len(line)-1 && isWordChar(rune(line[end+1])) {
		end++
	}

	return TextObject{
		Start: Position{Line: b.Cursor().Line, Col: start},
		End:   Position{Line: b.Cursor().Line, Col: end},
		Found: true,
	}
}

// AroundWord selects the word under cursor plus surrounding whitespace (aw).
func AroundWord(b *Buffer) TextObject {
	inner := InnerWord(b)
	if !inner.Found {
		return inner
	}

	line := b.CurrentLine()
	start := inner.Start.Col
	end := inner.End.Col

	// Prefer trailing whitespace
	if end < len(line)-1 && unicode.IsSpace(rune(line[end+1])) {
		for end < len(line)-1 && unicode.IsSpace(rune(line[end+1])) {
			end++
		}
	} else if start > 0 && unicode.IsSpace(rune(line[start-1])) {
		// Use leading whitespace if no trailing
		for start > 0 && unicode.IsSpace(rune(line[start-1])) {
			start--
		}
	}

	return TextObject{
		Start: Position{Line: b.Cursor().Line, Col: start},
		End:   Position{Line: b.Cursor().Line, Col: end},
		Found: true,
	}
}

// InnerQuote selects content inside quotes (i" or i').
func InnerQuote(b *Buffer, quote rune) TextObject {
	line := b.CurrentLine()
	col := b.Cursor().Col

	// Find opening quote
	start := -1
	for i := col; i >= 0; i-- {
		if rune(line[i]) == quote {
			start = i
			break
		}
	}

	if start == -1 {
		// Try finding opening quote after cursor
		for i := col; i < len(line); i++ {
			if rune(line[i]) == quote {
				start = i
				break
			}
		}
	}

	if start == -1 {
		return TextObject{Found: false}
	}

	// Find closing quote
	end := -1
	for i := start + 1; i < len(line); i++ {
		if rune(line[i]) == quote {
			end = i
			break
		}
	}

	if end == -1 {
		return TextObject{Found: false}
	}

	// Return content between quotes
	return TextObject{
		Start: Position{Line: b.Cursor().Line, Col: start + 1},
		End:   Position{Line: b.Cursor().Line, Col: end - 1},
		Found: end > start+1,
	}
}

// AroundQuote selects content including quotes (a" or a').
func AroundQuote(b *Buffer, quote rune) TextObject {
	line := b.CurrentLine()
	col := b.Cursor().Col

	// Find opening quote
	start := -1
	for i := col; i >= 0; i-- {
		if rune(line[i]) == quote {
			start = i
			break
		}
	}

	if start == -1 {
		for i := col; i < len(line); i++ {
			if rune(line[i]) == quote {
				start = i
				break
			}
		}
	}

	if start == -1 {
		return TextObject{Found: false}
	}

	// Find closing quote
	end := -1
	for i := start + 1; i < len(line); i++ {
		if rune(line[i]) == quote {
			end = i
			break
		}
	}

	if end == -1 {
		return TextObject{Found: false}
	}

	return TextObject{
		Start: Position{Line: b.Cursor().Line, Col: start},
		End:   Position{Line: b.Cursor().Line, Col: end},
		Found: true,
	}
}

// InnerBracket selects content inside brackets (i(, i), ib, i{, i}, iB, i[, i]).
func InnerBracket(b *Buffer, open, close rune) TextObject {
	content := b.Content()
	offset := b.CursorOffset()

	// Find opening bracket
	startOffset := findMatchingOpen(content, offset, open, close)
	if startOffset == -1 {
		return TextObject{Found: false}
	}

	// Find closing bracket
	endOffset := findMatchingClose(content, startOffset, open, close)
	if endOffset == -1 {
		return TextObject{Found: false}
	}

	// Convert to positions
	startPos := offsetToPosition(b, startOffset+1)
	endPos := offsetToPosition(b, endOffset-1)

	if endOffset <= startOffset+1 {
		return TextObject{Found: false}
	}

	return TextObject{
		Start: startPos,
		End:   endPos,
		Found: true,
	}
}

// AroundBracket selects content including brackets (a(, a), ab, a{, a}, aB, a[, a]).
func AroundBracket(b *Buffer, open, close rune) TextObject {
	content := b.Content()
	offset := b.CursorOffset()

	startOffset := findMatchingOpen(content, offset, open, close)
	if startOffset == -1 {
		return TextObject{Found: false}
	}

	endOffset := findMatchingClose(content, startOffset, open, close)
	if endOffset == -1 {
		return TextObject{Found: false}
	}

	return TextObject{
		Start: offsetToPosition(b, startOffset),
		End:   offsetToPosition(b, endOffset),
		Found: true,
	}
}

// Helper functions

func findMatchingOpen(content string, offset int, open, close rune) int {
	depth := 0

	for i := offset; i >= 0; i-- {
		ch := rune(content[i])
		if ch == close {
			depth++
		} else if ch == open {
			if depth == 0 {
				return i
			}
			depth--
		}
	}

	return -1
}

func findMatchingClose(content string, offset int, open, close rune) int {
	depth := 0

	for i := offset; i < len(content); i++ {
		ch := rune(content[i])
		if ch == open {
			depth++
		} else if ch == close {
			if depth == 1 {
				return i
			}
			depth--
		}
	}

	return -1
}

func offsetToPosition(b *Buffer, offset int) Position {
	if offset < 0 {
		return Position{Line: 0, Col: 0}
	}

	currentOffset := 0
	for i, line := range b.Lines() {
		lineEnd := currentOffset + len(line)
		if offset <= lineEnd {
			return Position{Line: i, Col: offset - currentOffset}
		}
		currentOffset = lineEnd + 1
	}

	// Beyond end
	lastLine := b.LineCount() - 1
	return Position{Line: lastLine, Col: len(b.Lines()[lastLine])}
}
