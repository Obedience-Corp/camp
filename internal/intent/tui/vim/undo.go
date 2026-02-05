package vim

// UndoEntry represents a single undoable change.
type UndoEntry struct {
	Content string   // Buffer content before the change
	Cursor  Position // Cursor position before the change
}

// UndoStack manages undo/redo history.
type UndoStack struct {
	undos   []UndoEntry
	redos   []UndoEntry
	maxSize int
}

// NewUndoStack creates a new undo stack with the given maximum size.
func NewUndoStack(maxSize int) *UndoStack {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &UndoStack{
		undos:   make([]UndoEntry, 0, maxSize),
		redos:   make([]UndoEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Save saves the current state for undo.
func (s *UndoStack) Save(content string, cursor Position) {
	entry := UndoEntry{
		Content: content,
		Cursor:  cursor,
	}

	s.undos = append(s.undos, entry)

	// Trim if over max size
	if len(s.undos) > s.maxSize {
		s.undos = s.undos[1:]
	}

	// Clear redo stack on new change
	s.redos = s.redos[:0]
}

// Undo returns the previous state, or nil if nothing to undo.
func (s *UndoStack) Undo(currentContent string, currentCursor Position) *UndoEntry {
	if len(s.undos) == 0 {
		return nil
	}

	// Pop from undo stack
	entry := s.undos[len(s.undos)-1]
	s.undos = s.undos[:len(s.undos)-1]

	// Push current state to redo stack
	s.redos = append(s.redos, UndoEntry{
		Content: currentContent,
		Cursor:  currentCursor,
	})

	return &entry
}

// Redo returns the next state, or nil if nothing to redo.
func (s *UndoStack) Redo(currentContent string, currentCursor Position) *UndoEntry {
	if len(s.redos) == 0 {
		return nil
	}

	// Pop from redo stack
	entry := s.redos[len(s.redos)-1]
	s.redos = s.redos[:len(s.redos)-1]

	// Push current state to undo stack
	s.undos = append(s.undos, UndoEntry{
		Content: currentContent,
		Cursor:  currentCursor,
	})

	return &entry
}

// CanUndo returns true if there are entries to undo.
func (s *UndoStack) CanUndo() bool {
	return len(s.undos) > 0
}

// CanRedo returns true if there are entries to redo.
func (s *UndoStack) CanRedo() bool {
	return len(s.redos) > 0
}

// Clear clears both undo and redo stacks.
func (s *UndoStack) Clear() {
	s.undos = s.undos[:0]
	s.redos = s.redos[:0]
}
