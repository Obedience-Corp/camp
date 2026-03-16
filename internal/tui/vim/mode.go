// Package vim provides vim-style modal editing for BubbleTea text input.
package vim

// Mode represents the current vim editing mode.
type Mode int

const (
	// ModeNormal is the default mode for navigation and commands.
	ModeNormal Mode = iota
	// ModeInsert allows text entry.
	ModeInsert
	// ModeVisual allows character-wise selection.
	ModeVisual
	// ModeVisualLine allows line-wise selection.
	ModeVisualLine
	// ModeCommand is for ex-style commands (e.g., :wq).
	ModeCommand
)

// String returns the string representation of the mode.
func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "V-LINE"
	case ModeCommand:
		return "COMMAND"
	default:
		return "UNKNOWN"
	}
}

// Operator represents a pending vim operator (d, c, y, etc.).
type Operator int

const (
	OpNone Operator = iota
	OpDelete
	OpChange
	OpYank
)

// String returns the string representation of the operator.
func (o Operator) String() string {
	switch o {
	case OpNone:
		return ""
	case OpDelete:
		return "d"
	case OpChange:
		return "c"
	case OpYank:
		return "y"
	default:
		return ""
	}
}

// State holds the current vim editing state.
type State struct {
	Mode          Mode
	PendingOp     Operator
	Count         int  // Numeric count prefix (e.g., 3dw)
	PendingMotion bool // Waiting for a motion after operator
	FindChar      rune // Character for f/F/t/T motions
	FindForward   bool // Direction for find
	FindTill      bool // Stop before (t/T) vs on (f/F) the character
	LastFindChar  rune // Last character used in f/F/t/T
	LastFindFwd   bool
	LastFindTill  bool
	VisualStart   int // Start position for visual mode
	CommandBuffer string

	// Multi-key sequence state
	PendingKey      rune     // For g (gg), z (zz), etc.
	AwaitingChar    bool     // Waiting for character input (f/F/t/T)
	AwaitingReplace bool     // Waiting for replacement character (r)
	AwaitingTextObj bool     // Waiting for text object type (diw, ci", etc.)
	TextObjInner    bool     // Inner vs around for text objects
	TextObjOperator Operator // Operator to apply with text object
}

// NewState creates a new vim state in normal mode.
func NewState() *State {
	return &State{
		Mode:  ModeNormal,
		Count: 1,
	}
}

// Reset clears pending operations and returns to normal mode.
func (s *State) Reset() {
	s.Mode = ModeNormal
	s.PendingOp = OpNone
	s.Count = 1
	s.PendingMotion = false
	s.FindChar = 0
	s.CommandBuffer = ""
	s.PendingKey = 0
	s.AwaitingChar = false
	s.AwaitingReplace = false
	s.AwaitingTextObj = false
	s.TextObjInner = false
	s.TextObjOperator = OpNone
}

// EnterInsert switches to insert mode.
func (s *State) EnterInsert() {
	s.Mode = ModeInsert
	s.PendingOp = OpNone
	s.Count = 1
	s.PendingMotion = false
}

// EnterVisual switches to visual mode.
func (s *State) EnterVisual(cursorPos int) {
	s.Mode = ModeVisual
	s.VisualStart = cursorPos
	s.PendingOp = OpNone
}

// EnterVisualLine switches to visual line mode.
func (s *State) EnterVisualLine(cursorPos int) {
	s.Mode = ModeVisualLine
	s.VisualStart = cursorPos
	s.PendingOp = OpNone
}

// EnterCommand switches to command mode.
func (s *State) EnterCommand() {
	s.Mode = ModeCommand
	s.CommandBuffer = ""
}

// SetOperator sets a pending operator and waits for motion.
func (s *State) SetOperator(op Operator) {
	s.PendingOp = op
	s.PendingMotion = true
}

// HasPendingOperator returns true if there's a pending operator.
func (s *State) HasPendingOperator() bool {
	return s.PendingOp != OpNone && s.PendingMotion
}

// ClearOperator clears the pending operator.
func (s *State) ClearOperator() {
	s.PendingOp = OpNone
	s.PendingMotion = false
	s.Count = 1
}

// AccumulateCount accumulates a digit into the count prefix.
func (s *State) AccumulateCount(digit int) {
	if s.Count == 1 && digit == 0 {
		// Leading zero is 0 motion (go to line start), not count
		return
	}
	if s.Count == 1 {
		s.Count = digit
	} else {
		s.Count = s.Count*10 + digit
	}
}

// GetCount returns the current count and resets it.
func (s *State) GetCount() int {
	count := s.Count
	s.Count = 1
	return count
}

// VisualRange returns the start and end of the visual selection.
// The returned range is always ordered (start <= end).
func (s *State) VisualRange(cursorPos int) (start, end int) {
	if s.VisualStart <= cursorPos {
		return s.VisualStart, cursorPos
	}
	return cursorPos, s.VisualStart
}

// IsVisual returns true if in any visual mode.
func (s *State) IsVisual() bool {
	return s.Mode == ModeVisual || s.Mode == ModeVisualLine
}
