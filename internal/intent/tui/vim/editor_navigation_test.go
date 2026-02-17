package vim

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditor_BasicNavigation(t *testing.T) {
	e := NewEditor("hello\nworld")

	// Test hjkl navigation
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if e.Cursor().Col != 1 {
		t.Errorf("l: Cursor().Col = %d, want 1", e.Cursor().Col)
	}

	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if e.Cursor().Line != 1 {
		t.Errorf("j: Cursor().Line = %d, want 1", e.Cursor().Line)
	}

	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if e.Cursor().Col != 0 {
		t.Errorf("h: Cursor().Col = %d, want 0", e.Cursor().Col)
	}

	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if e.Cursor().Line != 0 {
		t.Errorf("k: Cursor().Line = %d, want 0", e.Cursor().Line)
	}
}

func TestEditor_WordMotions(t *testing.T) {
	e := NewEditor("hello world test")

	// Test w motion
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if e.Cursor().Col != 6 {
		t.Errorf("w: Cursor().Col = %d, want 6", e.Cursor().Col)
	}

	// Test b motion
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if e.Cursor().Col != 0 {
		t.Errorf("b: Cursor().Col = %d, want 0", e.Cursor().Col)
	}

	// Test e motion
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if e.Cursor().Col != 4 {
		t.Errorf("e: Cursor().Col = %d, want 4", e.Cursor().Col)
	}
}

func TestEditor_LineNavigation(t *testing.T) {
	e := NewEditor("  hello world")

	// Test $ (end of line)
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'$'}})
	if e.Cursor().Col != 12 {
		t.Errorf("$: Cursor().Col = %d, want 12", e.Cursor().Col)
	}

	// Test 0 (start of line)
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	if e.Cursor().Col != 0 {
		t.Errorf("0: Cursor().Col = %d, want 0", e.Cursor().Col)
	}

	// Test ^ (first non-blank)
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'^'}})
	if e.Cursor().Col != 2 {
		t.Errorf("^: Cursor().Col = %d, want 2", e.Cursor().Col)
	}
}

func TestEditor_GG_Motion(t *testing.T) {
	e := NewEditor("line1\nline2\nline3\nline4")
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if e.Cursor().Line != 3 {
		t.Errorf("G: Cursor().Line = %d, want 3", e.Cursor().Line)
	}

	// Test gg (document start)
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if e.Cursor().Line != 0 {
		t.Errorf("gg: Cursor().Line = %d, want 0", e.Cursor().Line)
	}
}

func TestEditor_FindChar(t *testing.T) {
	e := NewEditor("hello world")

	// Test f motion
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if e.Cursor().Col != 4 {
		t.Errorf("fo: Cursor().Col = %d, want 4", e.Cursor().Col)
	}

	// Test ; (repeat find)
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{';'}})
	if e.Cursor().Col != 7 {
		t.Errorf(";: Cursor().Col = %d, want 7", e.Cursor().Col)
	}
}

func TestEditor_MatchBracket(t *testing.T) {
	pressPercent := func(e *Editor) {
		e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	}
	moveTo := func(e *Editor, col int) {
		// Reset to start then move right
		e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
		for range col {
			e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		}
	}

	tests := []struct {
		name     string
		content  string
		startCol int
		wantLine int
		wantCol  int
	}{
		{
			name:     "open paren to close",
			content:  "foo(bar)",
			startCol: 3,
			wantLine: 0, wantCol: 7,
		},
		{
			name:     "close paren to open",
			content:  "foo(bar)",
			startCol: 7,
			wantLine: 0, wantCol: 3,
		},
		{
			name:     "open brace to close",
			content:  "{hello}",
			startCol: 0,
			wantLine: 0, wantCol: 6,
		},
		{
			name:     "close brace to open",
			content:  "{hello}",
			startCol: 6,
			wantLine: 0, wantCol: 0,
		},
		{
			name:     "open bracket to close",
			content:  "a[b]c",
			startCol: 1,
			wantLine: 0, wantCol: 3,
		},
		{
			name:     "close bracket to open",
			content:  "a[b]c",
			startCol: 3,
			wantLine: 0, wantCol: 1,
		},
		{
			name:     "nested parens outer",
			content:  "((a))",
			startCol: 0,
			wantLine: 0, wantCol: 4,
		},
		{
			name:     "nested parens inner",
			content:  "((a))",
			startCol: 1,
			wantLine: 0, wantCol: 3,
		},
		{
			name:     "not on bracket scans forward",
			content:  "foo(bar)",
			startCol: 0,
			wantLine: 0, wantCol: 7,
		},
		{
			name:     "no bracket on line",
			content:  "hello",
			startCol: 0,
			wantLine: 0, wantCol: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEditor(tt.content)
			moveTo(e, tt.startCol)
			pressPercent(e)
			if e.Cursor().Line != tt.wantLine || e.Cursor().Col != tt.wantCol {
				t.Errorf("got (%d,%d), want (%d,%d)",
					e.Cursor().Line, e.Cursor().Col, tt.wantLine, tt.wantCol)
			}
		})
	}
}

func TestEditor_MatchBracketMultiLine(t *testing.T) {
	e := NewEditor("(\nhello\n)")

	// Cursor starts on ( at line 0, col 0
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	if e.Cursor().Line != 2 || e.Cursor().Col != 0 {
		t.Errorf("forward: got (%d,%d), want (2,0)", e.Cursor().Line, e.Cursor().Col)
	}

	// Now jump back
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	if e.Cursor().Line != 0 || e.Cursor().Col != 0 {
		t.Errorf("backward: got (%d,%d), want (0,0)", e.Cursor().Line, e.Cursor().Col)
	}
}
