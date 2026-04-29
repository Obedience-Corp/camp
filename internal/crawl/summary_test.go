package crawl

import "testing"

func TestSummary_RecordsKeepSkipMove(t *testing.T) {
	s := NewSummary()
	s.RecordKeep()
	s.RecordSkip()
	s.RecordMove("ready", ".campaign/intents/ready/a.md")
	s.RecordMove("ready", ".campaign/intents/ready/b.md")
	s.RecordMove("dungeon/done", ".campaign/intents/dungeon/done/c.md")

	if s.Kept != 1 {
		t.Errorf("Kept = %d, want 1", s.Kept)
	}
	if s.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", s.Skipped)
	}
	if s.Moved["ready"] != 2 || s.Moved["dungeon/done"] != 1 {
		t.Errorf("Moved = %v", s.Moved)
	}
	if got := len(s.Paths["ready"]); got != 2 {
		t.Errorf("Paths[ready] len = %d, want 2", got)
	}
}

func TestSummary_HasMovesAndTotals(t *testing.T) {
	s := NewSummary()
	if s.HasMoves() {
		t.Error("HasMoves on empty summary should be false")
	}
	if s.Total() != 0 {
		t.Errorf("Total empty = %d, want 0", s.Total())
	}

	s.RecordKeep()
	if s.HasMoves() {
		t.Error("HasMoves with only keep should be false")
	}

	s.RecordMove("ready", "a.md")
	if !s.HasMoves() {
		t.Error("HasMoves after move should be true")
	}
	if s.MovedTotal() != 1 {
		t.Errorf("MovedTotal = %d, want 1", s.MovedTotal())
	}
	if s.Total() != 2 {
		t.Errorf("Total = %d, want 2", s.Total())
	}
}

func TestSummary_RecordMoveSkipsEmptyPath(t *testing.T) {
	s := NewSummary()
	s.RecordMove("ready", "")
	if s.Moved["ready"] != 1 {
		t.Errorf("Moved should still tally count without a path")
	}
	if len(s.Paths["ready"]) != 0 {
		t.Errorf("Paths should not include empty entry")
	}
}
