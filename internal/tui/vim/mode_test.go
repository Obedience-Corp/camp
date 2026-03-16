package vim

import (
	"testing"
)

func TestMode_String(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeNormal, "NORMAL"},
		{ModeInsert, "INSERT"},
		{ModeVisual, "VISUAL"},
		{ModeVisualLine, "V-LINE"},
		{ModeCommand, "COMMAND"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("Mode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestState_NewState(t *testing.T) {
	s := NewState()
	if s.Mode != ModeNormal {
		t.Errorf("NewState().Mode = %v, want ModeNormal", s.Mode)
	}
	if s.Count != 1 {
		t.Errorf("NewState().Count = %d, want 1", s.Count)
	}
}

func TestState_EnterInsert(t *testing.T) {
	s := NewState()
	s.SetOperator(OpDelete)
	s.EnterInsert()

	if s.Mode != ModeInsert {
		t.Errorf("Mode = %v, want ModeInsert", s.Mode)
	}
	if s.PendingOp != OpNone {
		t.Errorf("PendingOp = %v, want OpNone", s.PendingOp)
	}
}

func TestState_EnterVisual(t *testing.T) {
	s := NewState()
	s.EnterVisual(10)

	if s.Mode != ModeVisual {
		t.Errorf("Mode = %v, want ModeVisual", s.Mode)
	}
	if s.VisualStart != 10 {
		t.Errorf("VisualStart = %d, want 10", s.VisualStart)
	}
}

func TestState_AccumulateCount(t *testing.T) {
	s := NewState()

	s.AccumulateCount(3)
	if s.Count != 3 {
		t.Errorf("Count = %d, want 3", s.Count)
	}

	s.AccumulateCount(5)
	if s.Count != 35 {
		t.Errorf("Count = %d, want 35", s.Count)
	}
}

func TestState_AccumulateCount_LeadingZero(t *testing.T) {
	s := NewState()

	// Leading zero should not change count (it's the 0 motion)
	s.AccumulateCount(0)
	if s.Count != 1 {
		t.Errorf("Count = %d, want 1 (leading zero ignored)", s.Count)
	}
}

func TestState_VisualRange(t *testing.T) {
	s := NewState()
	s.EnterVisual(5)

	// Cursor after start
	start, end := s.VisualRange(10)
	if start != 5 || end != 10 {
		t.Errorf("VisualRange(10) = (%d, %d), want (5, 10)", start, end)
	}

	// Cursor before start
	start, end = s.VisualRange(2)
	if start != 2 || end != 5 {
		t.Errorf("VisualRange(2) = (%d, %d), want (2, 5)", start, end)
	}
}

func TestState_SetOperator(t *testing.T) {
	s := NewState()
	s.SetOperator(OpDelete)

	if s.PendingOp != OpDelete {
		t.Errorf("PendingOp = %v, want OpDelete", s.PendingOp)
	}
	if !s.HasPendingOperator() {
		t.Error("HasPendingOperator() = false, want true")
	}

	s.ClearOperator()
	if s.HasPendingOperator() {
		t.Error("HasPendingOperator() = true after clear, want false")
	}
}

func TestState_Reset(t *testing.T) {
	s := NewState()
	s.EnterVisual(10)
	s.SetOperator(OpChange)
	s.AccumulateCount(5)
	s.EnterCommand()
	s.CommandBuffer = "wq"

	s.Reset()

	if s.Mode != ModeNormal {
		t.Errorf("Mode = %v after reset, want ModeNormal", s.Mode)
	}
	if s.PendingOp != OpNone {
		t.Errorf("PendingOp = %v after reset, want OpNone", s.PendingOp)
	}
	if s.Count != 1 {
		t.Errorf("Count = %d after reset, want 1", s.Count)
	}
	if s.CommandBuffer != "" {
		t.Errorf("CommandBuffer = %q after reset, want empty", s.CommandBuffer)
	}
}
