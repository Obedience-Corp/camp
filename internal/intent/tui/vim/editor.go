package vim

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// Editor is a vim-style text editor component.
type Editor struct {
	buffer       *Buffer
	state        *State
	undoStack    *UndoStack
	width        int
	height       int
	scrollOffset int    // First visible line
	lastChange   string // For . command
}

// NewEditor creates a new vim editor with the given content.
func NewEditor(content string) *Editor {
	return &Editor{
		buffer:    NewBuffer(content),
		state:     NewState(),
		undoStack: NewUndoStack(100),
		width:     80,
		height:    10,
	}
}

// Content returns the current buffer content.
func (e *Editor) Content() string {
	return e.buffer.Content()
}

// SetContent replaces the buffer content.
func (e *Editor) SetContent(content string) {
	e.buffer.SetContent(content)
}

// Mode returns the current vim mode.
func (e *Editor) Mode() Mode {
	return e.state.Mode
}

// Cursor returns the current cursor position.
func (e *Editor) Cursor() Position {
	return e.buffer.Cursor()
}

// CursorOffset returns the absolute cursor offset.
func (e *Editor) CursorOffset() int {
	return e.buffer.CursorOffset()
}

// SetSize sets the editor dimensions.
func (e *Editor) SetSize(width, height int) {
	e.width = width
	e.height = height
}

// CommandBuffer returns the current command buffer (for : commands).
func (e *Editor) CommandBuffer() string {
	return e.state.CommandBuffer
}

// IsCommandMode returns true if in command mode.
func (e *Editor) IsCommandMode() bool {
	return e.state.Mode == ModeCommand
}

// VisualSelection returns the visual selection range, if in visual mode.
func (e *Editor) VisualSelection() (start, end Position, active bool) {
	if !e.state.IsVisual() {
		return Position{}, Position{}, false
	}
	s, endOff := e.state.VisualRange(e.buffer.CursorOffset())

	// Convert offsets to positions
	startPos := e.offsetToPosition(s)
	endPos := e.offsetToPosition(endOff)

	return startPos, endPos, true
}

func (e *Editor) offsetToPosition(offset int) Position {
	currentOffset := 0
	for i, line := range e.buffer.Lines() {
		lineEnd := currentOffset + len(line)
		if offset <= lineEnd {
			return Position{Line: i, Col: offset - currentOffset}
		}
		currentOffset = lineEnd + 1
	}
	lastLine := e.buffer.LineCount() - 1
	return Position{Line: lastLine, Col: len(e.buffer.Lines()[lastLine])}
}

// Update handles key events and returns the updated editor.
func (e *Editor) Update(msg tea.KeyMsg) (cmd string, quit bool) {
	var result string
	var q bool

	switch e.state.Mode {
	case ModeNormal:
		result, q = e.handleNormal(msg)
	case ModeInsert:
		result, q = e.handleInsert(msg)
	case ModeVisual, ModeVisualLine:
		result, q = e.handleVisual(msg)
	case ModeCommand:
		result, q = e.handleCommand(msg)
	}

	// Ensure cursor stays visible after any operation
	e.EnsureCursorVisible()

	return result, q
}

// State returns the current vim state (for external inspection).
func (e *Editor) State() *State {
	return e.state
}

// handleNormal processes keys in normal mode.
func (e *Editor) handleNormal(msg tea.KeyMsg) (cmd string, quit bool) {
	key := msg.String()

	// Handle awaiting character input (f/F/t/T)
	if e.state.AwaitingChar {
		e.state.AwaitingChar = false
		if len(key) == 1 {
			char := rune(key[0])
			count := e.state.GetCount()
			if e.state.FindForward {
				FindCharForward(e.buffer, char, count, e.state.FindTill)
			} else {
				FindCharBackward(e.buffer, char, count, e.state.FindTill)
			}
			e.state.LastFindChar = char
			e.state.LastFindFwd = e.state.FindForward
			e.state.LastFindTill = e.state.FindTill
			e.EnsureCursorVisible()
		}
		return "", false
	}

	// Handle awaiting replacement character (r)
	if e.state.AwaitingReplace {
		e.state.AwaitingReplace = false
		if len(key) == 1 {
			e.saveUndo()
			e.buffer.ReplaceChar(rune(key[0]))
		}
		return "", false
	}

	// Handle awaiting text object (diw, ci", etc.)
	if e.state.AwaitingTextObj {
		return e.handleTextObjectKey(key)
	}

	// Handle pending key (gg)
	if e.state.PendingKey == 'g' {
		e.state.PendingKey = 0
		if key == "g" {
			MoveToDocumentStart(e.buffer)
			e.EnsureCursorVisible()
		}
		return "", false
	}

	// Handle counts
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		e.state.AccumulateCount(int(key[0] - '0'))
		return "", false
	}
	if len(key) == 1 && key[0] == '0' && e.state.Count > 1 {
		e.state.AccumulateCount(0)
		return "", false
	}

	count := e.state.GetCount()

	// Check for operator-pending mode
	if e.state.HasPendingOperator() {
		return e.handleOperatorPending(msg, count)
	}

	switch key {
	// Mode switching
	case "i":
		e.state.EnterInsert()
	case "a":
		MoveRight(e.buffer, 1)
		e.state.EnterInsert()
	case "I":
		MoveToFirstNonBlank(e.buffer)
		e.state.EnterInsert()
	case "A":
		MoveToLineEnd(e.buffer)
		MoveRight(e.buffer, 1) // Move past last char
		e.state.EnterInsert()
	case "o":
		e.saveUndo()
		e.buffer.NewLineBelow()
		e.state.EnterInsert()
	case "O":
		e.saveUndo()
		e.buffer.NewLineAbove()
		e.state.EnterInsert()
	case "s":
		e.saveUndo()
		e.buffer.DeleteChar()
		e.state.EnterInsert()
	case "S":
		e.saveUndo()
		line := e.buffer.CurrentLine()
		indent := getIndent(line)
		e.buffer.DeleteLine()
		e.buffer.Insert(indent)
		e.state.EnterInsert()
	case "C":
		e.saveUndo()
		e.buffer.DeleteToEndOfLine()
		e.state.EnterInsert()

	// Visual mode
	case "v":
		e.state.EnterVisual(e.buffer.CursorOffset())
	case "V":
		e.state.EnterVisualLine(e.buffer.CursorOffset())

	// Navigation
	case "h", "left":
		MoveLeft(e.buffer, count)
	case "l", "right":
		MoveRight(e.buffer, count)
	case "j", "down":
		MoveDown(e.buffer, count)
	case "k", "up":
		MoveUp(e.buffer, count)
	case "0":
		MoveToLineStart(e.buffer)
	case "$":
		MoveToLineEnd(e.buffer)
	case "^":
		MoveToFirstNonBlank(e.buffer)
	case "w":
		MoveWordForward(e.buffer, count)
	case "W":
		MoveBigWordForward(e.buffer, count)
	case "b":
		MoveWordBackward(e.buffer, count)
	case "B":
		MoveBigWordBackward(e.buffer, count)
	case "e":
		MoveWordEnd(e.buffer, count)
	case "E":
		MoveBigWordEnd(e.buffer, count)
	case "g":
		// Wait for next key (gg)
		e.state.PendingKey = 'g'
		return "", false
	case "G":
		if count > 1 {
			MoveToLine(e.buffer, count)
		} else {
			MoveToDocumentEnd(e.buffer)
		}
	case "{":
		MoveParagraphBackward(e.buffer, count)
	case "}":
		MoveParagraphForward(e.buffer, count)

	// Find
	case "f", "F", "t", "T":
		e.state.FindForward = key == "f" || key == "t"
		e.state.FindTill = key == "t" || key == "T"
		e.state.AwaitingChar = true
		return "", false
	case ";":
		if e.state.LastFindChar != 0 {
			if e.state.LastFindFwd {
				FindCharForward(e.buffer, e.state.LastFindChar, count, e.state.LastFindTill)
			} else {
				FindCharBackward(e.buffer, e.state.LastFindChar, count, e.state.LastFindTill)
			}
		}
	case ",":
		if e.state.LastFindChar != 0 {
			if !e.state.LastFindFwd {
				FindCharForward(e.buffer, e.state.LastFindChar, count, e.state.LastFindTill)
			} else {
				FindCharBackward(e.buffer, e.state.LastFindChar, count, e.state.LastFindTill)
			}
		}

	// Operators
	case "d":
		e.state.SetOperator(OpDelete)
	case "c":
		e.state.SetOperator(OpChange)
	case "y":
		e.state.SetOperator(OpYank)

	// Line operations
	case "x":
		e.saveUndo()
		for range count {
			e.buffer.DeleteChar()
		}
	case "X":
		e.saveUndo()
		for range count {
			e.buffer.DeleteCharBefore()
		}
	case "r":
		// Replace char - wait for next key
		e.state.AwaitingReplace = true
		return "", false
	case "J":
		e.saveUndo()
		for range count {
			e.buffer.JoinLines()
		}

	// Paste
	case "p":
		e.saveUndo()
		for range count {
			e.buffer.Paste()
		}
	case "P":
		e.saveUndo()
		for range count {
			e.buffer.PasteBefore()
		}

	// Undo/Redo
	case "u":
		e.undo()
	case "ctrl+r":
		e.redo()

	// Command mode
	case ":":
		e.state.EnterCommand()
	}

	return "", false
}

// handleOperatorPending processes keys when an operator is pending.
func (e *Editor) handleOperatorPending(msg tea.KeyMsg, count int) (string, bool) {
	key := msg.String()
	op := e.state.PendingOp

	// Double operator (dd, yy, cc)
	if (op == OpDelete && key == "d") ||
		(op == OpYank && key == "y") ||
		(op == OpChange && key == "c") {
		e.saveUndo()
		for range count {
			switch op {
			case OpDelete:
				e.buffer.DeleteLine()
			case OpYank:
				e.buffer.YankLine()
			case OpChange:
				e.buffer.DeleteLine()
				e.buffer.NewLineAbove()
				e.state.EnterInsert()
			}
		}
		e.state.ClearOperator()
		return "", false
	}

	// Motion after operator
	var motion Motion
	switch key {
	case "w":
		motion = MoveWordForward(e.buffer, count)
	case "W":
		motion = MoveBigWordForward(e.buffer, count)
	case "b":
		motion = MoveWordBackward(e.buffer, count)
	case "B":
		motion = MoveBigWordBackward(e.buffer, count)
	case "e":
		motion = MoveWordEnd(e.buffer, count)
	case "E":
		motion = MoveBigWordEnd(e.buffer, count)
	case "$":
		motion = MoveToLineEnd(e.buffer)
	case "0":
		motion = MoveToLineStart(e.buffer)
	case "^":
		motion = MoveToFirstNonBlank(e.buffer)
	case "h":
		motion = MoveLeft(e.buffer, count)
	case "l":
		motion = MoveRight(e.buffer, count)
	case "j":
		motion = MoveDown(e.buffer, count)
	case "k":
		motion = MoveUp(e.buffer, count)
	case "i":
		// Text object inner
		return e.handleTextObject(op, true, count)
	case "a":
		// Text object around
		return e.handleTextObject(op, false, count)
	default:
		e.state.ClearOperator()
		return "", false
	}

	// Apply operator to motion range
	e.applyOperator(op, motion.Start, motion.End)
	e.state.ClearOperator()
	return "", false
}

// handleTextObject sets up state to wait for text object key (iw, aw, i", etc.)
func (e *Editor) handleTextObject(op Operator, inner bool, count int) (string, bool) {
	e.state.AwaitingTextObj = true
	e.state.TextObjInner = inner
	e.state.TextObjOperator = op
	return "", false
}

// handleTextObjectKey handles the actual text object key (w, ", ', (, etc.)
func (e *Editor) handleTextObjectKey(key string) (string, bool) {
	e.state.AwaitingTextObj = false
	op := e.state.TextObjOperator
	inner := e.state.TextObjInner

	var obj TextObject

	switch key {
	case "w":
		if inner {
			obj = InnerWord(e.buffer)
		} else {
			obj = AroundWord(e.buffer)
		}
	case "W":
		// Big word - same as word for now
		if inner {
			obj = InnerWord(e.buffer)
		} else {
			obj = AroundWord(e.buffer)
		}
	case "\"":
		if inner {
			obj = InnerQuote(e.buffer, '"')
		} else {
			obj = AroundQuote(e.buffer, '"')
		}
	case "'":
		if inner {
			obj = InnerQuote(e.buffer, '\'')
		} else {
			obj = AroundQuote(e.buffer, '\'')
		}
	case "`":
		if inner {
			obj = InnerQuote(e.buffer, '`')
		} else {
			obj = AroundQuote(e.buffer, '`')
		}
	case "(", ")", "b":
		if inner {
			obj = InnerBracket(e.buffer, '(', ')')
		} else {
			obj = AroundBracket(e.buffer, '(', ')')
		}
	case "{", "}", "B":
		if inner {
			obj = InnerBracket(e.buffer, '{', '}')
		} else {
			obj = AroundBracket(e.buffer, '{', '}')
		}
	case "[", "]":
		if inner {
			obj = InnerBracket(e.buffer, '[', ']')
		} else {
			obj = AroundBracket(e.buffer, '[', ']')
		}
	case "<", ">":
		if inner {
			obj = InnerBracket(e.buffer, '<', '>')
		} else {
			obj = AroundBracket(e.buffer, '<', '>')
		}
	default:
		e.state.ClearOperator()
		return "", false
	}

	if obj.Found {
		e.applyOperator(op, obj.Start, obj.End)
	}
	e.state.ClearOperator()
	e.EnsureCursorVisible()
	return "", false
}

// applyOperator applies an operator to a range.
func (e *Editor) applyOperator(op Operator, start, end Position) {
	switch op {
	case OpDelete:
		e.saveUndo()
		e.buffer.DeleteRange(start, end)
	case OpChange:
		e.saveUndo()
		e.buffer.DeleteRange(start, end)
		e.state.EnterInsert()
	case OpYank:
		e.buffer.YankRange(start, end)
	}
}

// handleInsert processes keys in insert mode.
func (e *Editor) handleInsert(msg tea.KeyMsg) (cmd string, quit bool) {
	switch msg.Type {
	case tea.KeyEscape:
		// Exit insert mode
		e.state.Reset()
		// Move cursor back one if not at start
		if e.buffer.Cursor().Col > 0 {
			MoveLeft(e.buffer, 1)
		}
	case tea.KeyBackspace:
		e.buffer.DeleteCharBefore()
	case tea.KeyDelete:
		e.buffer.DeleteChar()
	case tea.KeyEnter:
		e.buffer.Insert("\n")
	case tea.KeyTab:
		e.buffer.Insert("\t")
	case tea.KeyRunes:
		e.buffer.Insert(string(msg.Runes))
	}
	return "", false
}

// handleVisual processes keys in visual mode.
func (e *Editor) handleVisual(msg tea.KeyMsg) (cmd string, quit bool) {
	key := msg.String()

	switch key {
	case "esc":
		e.state.Reset()
	case "h", "left":
		MoveLeft(e.buffer, 1)
	case "l", "right":
		MoveRight(e.buffer, 1)
	case "j", "down":
		MoveDown(e.buffer, 1)
	case "k", "up":
		MoveUp(e.buffer, 1)
	case "w":
		MoveWordForward(e.buffer, 1)
	case "b":
		MoveWordBackward(e.buffer, 1)
	case "e":
		MoveWordEnd(e.buffer, 1)
	case "0":
		MoveToLineStart(e.buffer)
	case "$":
		MoveToLineEnd(e.buffer)
	case "o":
		// Swap selection anchor
		curPos := e.buffer.CursorOffset()
		e.buffer.SetCursorFromOffset(e.state.VisualStart)
		e.state.VisualStart = curPos
	case "d", "x":
		start, end := e.state.VisualRange(e.buffer.CursorOffset())
		e.saveUndo()
		e.buffer.DeleteRange(e.offsetToPosition(start), e.offsetToPosition(end))
		e.state.Reset()
	case "y":
		start, end := e.state.VisualRange(e.buffer.CursorOffset())
		e.buffer.YankRange(e.offsetToPosition(start), e.offsetToPosition(end))
		e.state.Reset()
	case "c":
		start, end := e.state.VisualRange(e.buffer.CursorOffset())
		e.saveUndo()
		e.buffer.DeleteRange(e.offsetToPosition(start), e.offsetToPosition(end))
		e.state.EnterInsert()
	}

	return "", false
}

// handleCommand processes keys in command mode.
func (e *Editor) handleCommand(msg tea.KeyMsg) (cmd string, quit bool) {
	switch msg.Type {
	case tea.KeyEnter:
		cmd := e.state.CommandBuffer
		e.state.Reset()
		return cmd, false
	case tea.KeyEscape:
		e.state.Reset()
	case tea.KeyBackspace:
		if len(e.state.CommandBuffer) > 0 {
			e.state.CommandBuffer = e.state.CommandBuffer[:len(e.state.CommandBuffer)-1]
		} else {
			e.state.Reset()
		}
	case tea.KeyRunes:
		e.state.CommandBuffer += string(msg.Runes)
	}
	return "", false
}

// saveUndo saves the current state for undo.
func (e *Editor) saveUndo() {
	e.undoStack.Save(e.buffer.Content(), e.buffer.Cursor())
}

// undo reverts to the previous state.
func (e *Editor) undo() {
	if entry := e.undoStack.Undo(e.buffer.Content(), e.buffer.Cursor()); entry != nil {
		e.buffer.SetContent(entry.Content)
		e.buffer.SetCursor(entry.Cursor)
	}
}

// redo reapplies the next state.
func (e *Editor) redo() {
	if entry := e.undoStack.Redo(e.buffer.Content(), e.buffer.Cursor()); entry != nil {
		e.buffer.SetContent(entry.Content)
		e.buffer.SetCursor(entry.Cursor)
	}
}

// getIndent returns the leading whitespace of a line.
func getIndent(line string) string {
	var indent strings.Builder
	for _, r := range line {
		if unicode.IsSpace(r) {
			indent.WriteRune(r)
		} else {
			break
		}
	}
	return indent.String()
}
