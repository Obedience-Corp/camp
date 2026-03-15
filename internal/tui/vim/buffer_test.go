package vim

import (
	"testing"
)

func TestBuffer_NewBuffer(t *testing.T) {
	b := NewBuffer("hello\nworld")

	if len(b.Lines()) != 2 {
		t.Errorf("Lines() length = %d, want 2", len(b.Lines()))
	}
	if b.Lines()[0] != "hello" {
		t.Errorf("Lines()[0] = %q, want \"hello\"", b.Lines()[0])
	}
	if b.Lines()[1] != "world" {
		t.Errorf("Lines()[1] = %q, want \"world\"", b.Lines()[1])
	}
}

func TestBuffer_Content(t *testing.T) {
	original := "hello\nworld"
	b := NewBuffer(original)

	if got := b.Content(); got != original {
		t.Errorf("Content() = %q, want %q", got, original)
	}
}

func TestBuffer_SetCursor(t *testing.T) {
	b := NewBuffer("hello\nworld")

	b.SetCursor(Position{Line: 1, Col: 2})
	pos := b.Cursor()

	if pos.Line != 1 || pos.Col != 2 {
		t.Errorf("Cursor() = %+v, want {Line:1, Col:2}", pos)
	}
}

func TestBuffer_SetCursor_Bounds(t *testing.T) {
	b := NewBuffer("hello\nworld")

	// Beyond end
	b.SetCursor(Position{Line: 10, Col: 10})
	pos := b.Cursor()

	if pos.Line != 1 {
		t.Errorf("Cursor().Line = %d, want 1 (clamped)", pos.Line)
	}
	if pos.Col >= len("world") {
		t.Errorf("Cursor().Col = %d, should be clamped to line length", pos.Col)
	}
}

func TestBuffer_Insert(t *testing.T) {
	b := NewBuffer("hello")
	// Use insert mode cursor positioning to allow col == len(line)
	b.SetCursorInsert(Position{Line: 0, Col: 5})
	b.Insert(" world")

	if got := b.Content(); got != "hello world" {
		t.Errorf("Content() = %q, want \"hello world\"", got)
	}
}

func TestBuffer_Insert_Append(t *testing.T) {
	b := NewBuffer("hello")
	// Move to end of line using insert mode
	line := b.CurrentLine()
	b.SetCursorInsert(Position{Line: 0, Col: len(line)})
	b.Insert("!")

	if got := b.Content(); got != "hello!" {
		t.Errorf("Content() = %q, want \"hello!\"", got)
	}
}

func TestBuffer_Insert_Middle(t *testing.T) {
	b := NewBuffer("hllo")
	b.SetCursor(Position{Line: 0, Col: 1})
	b.Insert("e")

	if got := b.Content(); got != "hello" {
		t.Errorf("Content() = %q, want \"hello\"", got)
	}
}

func TestBuffer_DeleteChar(t *testing.T) {
	b := NewBuffer("hello")
	b.SetCursor(Position{Line: 0, Col: 2})
	deleted := b.DeleteChar()

	if deleted != "l" {
		t.Errorf("DeleteChar() = %q, want \"l\"", deleted)
	}
	if got := b.Content(); got != "helo" {
		t.Errorf("Content() = %q, want \"helo\"", got)
	}
}

func TestBuffer_DeleteCharBefore(t *testing.T) {
	b := NewBuffer("hello")
	b.SetCursor(Position{Line: 0, Col: 2})
	deleted := b.DeleteCharBefore()

	if deleted != "e" {
		t.Errorf("DeleteCharBefore() = %q, want \"e\"", deleted)
	}
	if got := b.Content(); got != "hllo" {
		t.Errorf("Content() = %q, want \"hllo\"", got)
	}
	if b.Cursor().Col != 1 {
		t.Errorf("Cursor().Col = %d, want 1", b.Cursor().Col)
	}
}

func TestBuffer_DeleteLine(t *testing.T) {
	b := NewBuffer("hello\nworld\ntest")
	b.SetCursor(Position{Line: 1, Col: 0})
	deleted := b.DeleteLine()

	if deleted != "world\n" {
		t.Errorf("DeleteLine() = %q, want \"world\\n\"", deleted)
	}
	if got := b.Content(); got != "hello\ntest" {
		t.Errorf("Content() = %q, want \"hello\\ntest\"", got)
	}
}

func TestBuffer_DeleteToEndOfLine(t *testing.T) {
	b := NewBuffer("hello world")
	b.SetCursor(Position{Line: 0, Col: 5})
	deleted := b.DeleteToEndOfLine()

	if deleted != " world" {
		t.Errorf("DeleteToEndOfLine() = %q, want \" world\"", deleted)
	}
	if got := b.Content(); got != "hello" {
		t.Errorf("Content() = %q, want \"hello\"", got)
	}
}

func TestBuffer_YankLine(t *testing.T) {
	b := NewBuffer("hello\nworld")
	b.SetCursor(Position{Line: 0, Col: 0})
	yanked := b.YankLine()

	if yanked != "hello\n" {
		t.Errorf("YankLine() = %q, want \"hello\\n\"", yanked)
	}
	if b.Yank() != yanked {
		t.Errorf("Yank() = %q, want %q", b.Yank(), yanked)
	}
}

func TestBuffer_Paste(t *testing.T) {
	b := NewBuffer("hello")
	b.SetYank(" world")
	b.SetCursor(Position{Line: 0, Col: 4})
	b.Paste()

	if got := b.Content(); got != "hello world" {
		t.Errorf("Content() = %q, want \"hello world\"", got)
	}
}

func TestBuffer_Paste_Line(t *testing.T) {
	b := NewBuffer("hello")
	b.SetYank("world\n")
	b.SetCursor(Position{Line: 0, Col: 0})
	b.Paste()

	if got := b.Content(); got != "hello\nworld" {
		t.Errorf("Content() = %q, want \"hello\\nworld\"", got)
	}
}

func TestBuffer_ReplaceChar(t *testing.T) {
	b := NewBuffer("hello")
	b.SetCursor(Position{Line: 0, Col: 1})
	b.ReplaceChar('a')

	if got := b.Content(); got != "hallo" {
		t.Errorf("Content() = %q, want \"hallo\"", got)
	}
}

func TestBuffer_JoinLines(t *testing.T) {
	b := NewBuffer("hello\nworld")
	b.SetCursor(Position{Line: 0, Col: 0})
	b.JoinLines()

	if got := b.Content(); got != "hello world" {
		t.Errorf("Content() = %q, want \"hello world\"", got)
	}
}

func TestBuffer_NewLineBelow(t *testing.T) {
	b := NewBuffer("hello\nworld")
	b.SetCursor(Position{Line: 0, Col: 0})
	b.NewLineBelow()

	if got := b.Content(); got != "hello\n\nworld" {
		t.Errorf("Content() = %q, want \"hello\\n\\nworld\"", got)
	}
	if b.Cursor().Line != 1 {
		t.Errorf("Cursor().Line = %d, want 1", b.Cursor().Line)
	}
}

func TestBuffer_NewLineAbove(t *testing.T) {
	b := NewBuffer("hello\nworld")
	b.SetCursor(Position{Line: 1, Col: 0})
	b.NewLineAbove()

	if got := b.Content(); got != "hello\n\nworld" {
		t.Errorf("Content() = %q, want \"hello\\n\\nworld\"", got)
	}
	if b.Cursor().Line != 1 {
		t.Errorf("Cursor().Line = %d, want 1", b.Cursor().Line)
	}
}

func TestBuffer_CursorOffset(t *testing.T) {
	b := NewBuffer("hello\nworld")
	b.SetCursor(Position{Line: 1, Col: 2})

	// "hello\n" = 6 chars, then "wo" = 2 more
	expected := 8
	if got := b.CursorOffset(); got != expected {
		t.Errorf("CursorOffset() = %d, want %d", got, expected)
	}
}

func TestBuffer_SetCursorFromOffset(t *testing.T) {
	b := NewBuffer("hello\nworld")
	b.SetCursorFromOffset(8) // Should be at "r" in "world"

	pos := b.Cursor()
	if pos.Line != 1 || pos.Col != 2 {
		t.Errorf("Cursor() = %+v, want {Line:1, Col:2}", pos)
	}
}

func TestBuffer_DeleteCharBefore_CursorBeyondLine(t *testing.T) {
	// Simulate cursor position exceeding line length (can happen in insert mode)
	b := NewBuffer("a")
	// Manually set cursor beyond line content to simulate the bug scenario
	b.cursor.Col = 18 // Line has length 1, cursor at 18

	// This should NOT panic - it should clamp and delete
	deleted := b.DeleteCharBefore()

	if deleted != "a" {
		t.Errorf("DeleteCharBefore() = %q, want \"a\"", deleted)
	}
	if got := b.Content(); got != "" {
		t.Errorf("Content() = %q, want \"\"", got)
	}
	if b.Cursor().Col != 0 {
		t.Errorf("Cursor().Col = %d, want 0", b.Cursor().Col)
	}
}

func TestBuffer_DeleteCharBefore_EmptyLine(t *testing.T) {
	b := NewBuffer("")
	b.cursor.Col = 0

	deleted := b.DeleteCharBefore()

	if deleted != "" {
		t.Errorf("DeleteCharBefore() on empty buffer = %q, want \"\"", deleted)
	}
}

func TestBuffer_DeleteCharBefore_JoinLines(t *testing.T) {
	b := NewBuffer("hello\nworld")
	b.SetCursor(Position{Line: 1, Col: 0})

	deleted := b.DeleteCharBefore()

	if deleted != "\n" {
		t.Errorf("DeleteCharBefore() at line start = %q, want \"\\n\"", deleted)
	}
	if got := b.Content(); got != "helloworld" {
		t.Errorf("Content() = %q, want \"helloworld\"", got)
	}
	if b.Cursor().Line != 0 || b.Cursor().Col != 5 {
		t.Errorf("Cursor() = %+v, want {Line:0, Col:5}", b.Cursor())
	}
}
